# Comment ownership fixtures

No comment belonging to a removed draft declaration may survive in the stable output:
neither one protoc attributes to it, nor one after it that protoc attaches to nothing.

- `attached`: comments attached to removed declarations are removed; its golden pins the
  raw printer output.
- Every other case adds one comment the generator must reject, because it would
  otherwise survive next to where a draft declaration was removed.
