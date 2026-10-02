# Contributing

## Field validation

Field constraints use standard [Buf Protovalidate annotations](buf/validate/validate.proto):

```proto
import "buf/validate/validate.proto";

// Inside a message:
string namespace = 1 [(buf.validate.field).required = true];
```

`required` rejects zero-valued ordinary proto3 scalars and empty lists/maps. Fields with explicit presence only require presence. Consuming applications enforce the annotations with Protovalidate. The upstream schema is vendored alongside the Google and Nexus dependencies and pinned in `buf.lock`.
