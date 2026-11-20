package main

import (
 "encoding/json"
 "strings"
 "testing"
 "time"
)
func TestOpenInferenceMessagesAndPrivacy(t *testing.T) {
 now:=time.Now(); tr:=&finishedTrace{StartedAt:now,CompletedAt:now,SessionID:"session",PromptJSON:[]byte(`{"messages":[{"role":"user","content":"hello"}]}`),Completion:[]part{{Type:partText,Content:"world"}},Attempts:[]usageRecord{{Provider:"openai",Model:"test",RequestedAt:now}}}
 cfg:=defaultConfig(); cfg.CaptureContent=true; b,_:=json.Marshal(buildTraceSpans(tr,cfg,now)); s:=string(b)
 for _,want:=range []string{"openinference.span.kind","llm.input_messages.0.message.content","llm.output_messages.0.message.content","hello","world"} {if !strings.Contains(s,want){t.Errorf("missing %s",want)}}
 cfg.CaptureContent=false; b,_=json.Marshal(buildTraceSpans(tr,cfg,now)); if strings.Contains(string(b),"hello")||strings.Contains(string(b),"world"){t.Fatal("content leaked when disabled")}
}
func TestProtobufEncodingRejectsInvalidIDs(t *testing.T) {
 req:=otlpRequest{ResourceSpans:[]otlpResourceSpans{{ScopeSpans:[]otlpScopeSpans{{Spans:[]otlpSpan{{TraceID:"invalid"}}}}}}}; if _,err:=encodeProtobuf(req);err==nil{t.Fatal("invalid ID accepted")}
}

func TestSessionTurnRoot(t *testing.T) {
 now := time.Now()
 tr := &finishedTrace{StartedAt: now, CompletedAt: now,
  PromptJSON: []byte(`{"messages":[{"role":"user","content":"old"},{"role":"assistant","content":"previous"},{"role":"user","content":"última pergunta"},{"role":"tool","content":"tool result"}]}`),
  Completion: []part{{Type:partReasoning,Content:"private thought"},{Type:partText,Content:"resposta final"}},
  Attempts: []usageRecord{{Provider:"openai",Model:"test",RequestedAt:now}},
 }
 cfg := defaultConfig(); cfg.CaptureContent = true
 spans := buildTraceSpans(tr,cfg,now)
 root := attrKeys(spans[0].Attributes)
 if root["input.value"] != "última pergunta" || root["output.value"] != "resposta final" { t.Fatalf("unexpected root values: %#v",root) }
 if root["input.mime_type"] != "text/plain" || root["output.mime_type"] != "text/plain" { t.Fatalf("missing plain text mime type: %#v",root) }
 if !strings.Contains(attrKeys(spans[1].Attributes)["gen_ai.input.messages"],"old") { t.Fatal("child lost conversation history") }
 tr.Attempts = nil
 root = attrKeys(buildTraceSpans(tr,cfg,now)[0].Attributes)
 if root["input.value"] != "última pergunta" || root["output.value"] != "resposta final" || root["gen_ai.input.messages"] == "" { t.Fatal("incorrect root without attempt") }
 cfg.CaptureContent = false
 for _, span := range buildTraceSpans(tr,cfg,now) { if attrKeys(span.Attributes)["input.value"] != "" || attrKeys(span.Attributes)["output.value"] != "" { t.Fatal("content leaked with capture disabled") } }
}

func TestSessionTurnToolCallAndLimit(t *testing.T) {
 var a attrs
 cfg:=defaultConfig(); cfg.MaxContentChars=20
 tr:=&finishedTrace{PromptJSON:[]byte(`{"messages":[{"role":"user","content":"áááááááááááááááááááá"}]}`),Completion:[]part{{Type:partToolCall,Name:"read_file",Arguments:[]byte(`{"path":"secret"}`)}}}
 addSessionTurnAttrs(&a,tr,cfg)
 got:=attrKeys(a)
 if len(got["input.value"])>20 || got["output.value"]!="Tool call: read_file" || strings.Contains(got["output.value"],"secret") { t.Fatalf("unexpected summaries: %#v",got) }
}

func TestResponsesContentAndFunctionItems(t *testing.T) {
 body := []byte(`{"input":[{"role":"user","content":[{"type":"input_text","text":"ping"}]},{"type":"function_call","id":"item_1","call_id":"call_1","name":"read","arguments":"{\"path\":\"x\"}"},{"type":"function_call_output","call_id":"call_1","output":"file content"},{"role":"assistant","content":[{"type":"output_text","text":"pong"}]}]}`)
 msgs := promptMessages(body)
 if len(msgs) != 4 { t.Fatalf("got %d messages: %#v",len(msgs),msgs) }
 if msgs[0].Parts[0].Content != "ping" || msgs[3].Parts[0].Content != "pong" { t.Fatalf("wrong text: %#v",msgs) }
 if msgs[1].Role != "assistant" || msgs[1].Parts[0].Type != partToolCall || msgs[1].Parts[0].ID != "call_1" { t.Fatalf("wrong function call: %#v",msgs[1]) }
 if msgs[2].Role != "tool" || msgs[2].Parts[0].ID != "call_1" || partToText(msgs[2].Parts[0]) != "file content" { t.Fatalf("wrong function result: %#v",msgs[2]) }
 cfg:=defaultConfig(); cfg.CaptureContent=true
 tr:=&finishedTrace{PromptJSON:body,Completion:[]part{{Type:partText,Content:"pong"}},Attempts:[]usageRecord{{}}}
 spans:=buildTraceSpans(tr,cfg,time.Now())
 if got:=attrKeys(spans[0].Attributes)["input.value"]; got!="ping" { t.Fatalf("root input: %q",got) }
 if got:=attrKeys(spans[1].Attributes)["llm.input_messages.0.message.content"]; got!="ping" { t.Fatalf("child input: %q",got) }
 for _,tc:=range []struct{typ,key,value string}{{"output_text","text","pong"},{"refusal","refusal","cannot"}} {
  p,ok:=blockToPart(map[string]any{"type":tc.typ,tc.key:tc.value}); if !ok || p.Type!=partText || p.Content!=tc.value { t.Errorf("%s: %#v",tc.typ,p) }
 }
}
