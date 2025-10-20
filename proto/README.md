# Protobuf Definitions for publisher

All of the `publisher` proto files are defined here. This folder is the canonical source for the protobuf
definitions used in the project.

These definitions are managed on the [Buf Schema Registry](httpss://buf.build) at
`buf.build/ssvlabs/publisher`.

User facing documentation should not be placed here but instead goes in `buf.md` and in each protobuf package following
the guidelines in https://docs.buf.build/bsr/documentation.

## Development

The `Makefile` in this directory provides several commands to work with the protobuf files.

### Generate

To generate the Go code from the `.proto` files, run:

```bash
make proto-gen
```

This will run `buf generate` and output the generated files in their respective directories.

### Lint

To lint the `.proto` files, run:

```bash
make proto-lint
```

### Format

To format the `.proto` files, run:

```bash
make proto-format
```

### Check for Breaking Changes

To check for breaking changes against the `main` branch, run:

```bash
make proto-breaking
```

### Update Dependencies

To update the protobuf dependencies, run:

```bash
make proto-deps
```

This should be done by a maintainer after changes have been merged to `main`.
