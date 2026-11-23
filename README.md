# Temporal proto files  

This repository contains both the protobuf descriptors and OpenAPI documentation for the Temporal platform.

## How to use

Install as git submodule to the project.

## Consumers

Compile either `temporal/api` (stable) or `temporal/api_next` (preview). Never compile both together.
The trees declare the same proto packages, so compiling both causes duplicate symbols.
The default Buf module in `buf.yaml` excludes `temporal/api_next`.
Use `buf.next.yaml` to compile the preview tree.

## Contribution

Edit proto files under `temporal/api_next/`. Run `make stable-api` to regenerate `temporal/api/`, or run
`make` to also update the OpenAPI definitions. Do not edit `temporal/api/` by hand. Commit the regenerated
tree with your changes because CI fails if it is stale. This also regenerates
`temporal/api/draft-manifest.json`, which records the resolved feature gate for each draft
declaration for Go code generation.

Use the `draft` field option or the `draft_message` message option from
[`temporal/api_next/protometa/v1/draft.proto`](temporal/api_next/protometa/v1/draft.proto) to mark draft
declarations. For example, `string user = 7 [(temporal.api.protometa.v1.draft) = "update-user-tagging"];`
is a draft field. The generator removes annotated declarations from the stable tree.
See the [draft API specification](docs/draft-apis.md) for annotation rules, runtime behavior, and implementation gaps.

Rust is also required because `make` installs and runs `nex-gen` when regenerating system Nexus WIT files.

## Breaking changes

Sometimes during initial feature development, there will be breaking API changes made. Running `make` will
catch these changes and fail CI. If the breaking change is for a feature not yet released, a temporary `ignore`
line can be added to `buf.yaml` to pass CI. This is
[an example](https://github.com/temporalio/api/pull/608/files#diff-1a5ba9cba93e971f532139f694d7da802776bfe578e3f753b9c3f25968dbf42dL16)
of adding such an exception. A subsequent PR will be needed to then disable to exception once it's been merged in.

## License

MIT License, please see [LICENSE](LICENSE) for details.
