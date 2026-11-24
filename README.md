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

Edit `temporal/api_next/` for all schema changes. The generator writes the stable schema to
`temporal/api/`. Do not edit that folder by hand.

Use a [draft annotation](docs/draft-apis.md) to keep a declaration out of the stable schema while
you develop it. Remove the annotation when the declaration is ready to ship.

Run `make` to regenerate the stable protos, draft manifest, and OpenAPI definitions. To update
just `temporal/api/`, run `make stable-api`. Commit the source changes and generated files together.

Rust is also required because `make` installs and runs `nex-gen` when regenerating system Nexus WIT files.

## Breaking changes

Sometimes during initial feature development, there will be breaking API changes made. Running `make` will
catch these changes and fail CI. If the breaking change is for a feature not yet released, a temporary `ignore`
line can be added to `buf.yaml` to pass CI. This is
[an example](https://github.com/temporalio/api/pull/608/files#diff-1a5ba9cba93e971f532139f694d7da802776bfe578e3f753b9c3f25968dbf42dL16)
of adding such an exception. A subsequent PR will be needed to then disable to exception once it's been merged in.

## License

MIT License, please see [LICENSE](LICENSE) for details.
