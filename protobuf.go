package main

// Minimal OTLP trace protobuf encoder for the wire types this plugin emits.
// Field numbers follow opentelemetry-proto; unknown fields are not generated.
import (
 "encoding/binary"
 "encoding/hex"
 "fmt"
 "math"
 "strconv"
)

type wire []byte
func (w wire) key(n int, typ byte) wire { return w.varint(uint64(n<<3)|uint64(typ)) }
func (w wire) varint(v uint64) wire { for v >= 128 { w=append(w,byte(v)|0x80); v >>= 7 }; return append(w,byte(v)) }
func (w wire) data(n int, b []byte) wire { w=w.key(n,2).varint(uint64(len(b))); return append(w,b...) }
func (w wire) str(n int, s string) wire { if s=="" { return w }; return w.data(n,[]byte(s)) }
func (w wire) number(n int, v uint64) wire { return w.key(n,0).varint(v) }
func (w wire) fixed(n int, v uint64) wire { w=w.key(n,1); var b [8]byte; binary.LittleEndian.PutUint64(b[:],v); return append(w,b[:]...) }
func protobufAttrs(as []otlpKeyValue) []wire { out:=make([]wire,0,len(as)); for _,a:=range as { var val wire; v:=a.Value; switch { case v.StringValue!=nil: val=val.data(1,[]byte(*v.StringValue)); case v.BoolValue!=nil: var b uint64; if *v.BoolValue { b=1 }; val=val.number(2,b); case v.IntValue!=nil: i,_:=strconv.ParseInt(*v.IntValue,10,64); val=val.number(3,uint64(i)); case v.DoubleValue!=nil: val=val.fixed(4,math.Float64bits(*v.DoubleValue)); default: continue }; var kv wire; kv=kv.str(1,a.Key).data(2,val); out=append(out,kv) }; return out }
func appendAttrs(w wire, n int, a []otlpKeyValue) wire { for _,v:=range protobufAttrs(a) { w=w.data(n,v) }; return w }
func encodeProtobuf(req otlpRequest) ([]byte,error) {
 var result wire
 for _,rs:=range req.ResourceSpans { var resource, r wire; resource=appendAttrs(resource,1,rs.Resource.Attributes); r=r.data(1,resource)
  for _,ss:=range rs.ScopeSpans { var scope, sc wire; scope=scope.str(1,ss.Scope.Name).str(2,ss.Scope.Version); sc=sc.data(1,scope)
   for _,s:=range ss.Spans { var sp wire
    trace,err:=hex.DecodeString(s.TraceID); if err!=nil || len(trace)!=16 { return nil,fmt.Errorf("invalid trace id") }
    id,err:=hex.DecodeString(s.SpanID); if err!=nil || len(id)!=8 { return nil,fmt.Errorf("invalid span id") }
    sp=sp.data(1,trace).data(2,id)
    if s.ParentSpanID!="" { parent,err:=hex.DecodeString(s.ParentSpanID); if err!=nil || len(parent)!=8 { return nil,fmt.Errorf("invalid parent span id") }; sp=sp.data(4,parent) }
    start,err:=strconv.ParseUint(s.StartTimeUnixNano,10,64); if err!=nil{return nil,err}; end,err:=strconv.ParseUint(s.EndTimeUnixNano,10,64); if err!=nil{return nil,err}
    sp=sp.str(5,s.Name).number(6,uint64(s.Kind)).fixed(7,start).fixed(8,end); sp=appendAttrs(sp,9,s.Attributes)
    for _,event:=range s.Events { var ev wire; t,err:=strconv.ParseUint(event.TimeUnixNano,10,64); if err!=nil{return nil,err}; ev=ev.fixed(1,t).str(2,event.Name); ev=appendAttrs(ev,3,event.Attributes); sp=sp.data(11,ev) }
    var status wire; status=status.str(2,s.Status.Message); if s.Status.Code!=0 { status=status.number(3,uint64(s.Status.Code)) }; sp=sp.data(15,status); sc=sc.data(2,sp)
   }; r=r.data(2,sc)
  }; result=result.data(1,r)
 }; return result,nil
}
