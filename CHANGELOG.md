# nixcached Release Notes

The format is based on [Keep a Changelog][],
and this project adheres to [Semantic Versioning][].

[Keep a Changelog]: https://keepachangelog.com/en/1.0.0/
[Semantic Versioning]: https://semver.org/spec/v2.0.0.html
[Unreleased]: https://github.com/zombiezen/nixcached/compare/v0.1.1...HEAD

## [Unreleased][]

### Added

- Uploads are now performed concurrently, defaulting to two at a time.
  The number can be tuned with the `upload --jobs` flag.

## [0.1.1][] - 2023-07-27

Version 0.1.1 has no changes from 0.1.0
but fixes the 64-bit ARM Docker image.

[0.1.1]: https://github.com/zombiezen/nixcached/releases/tag/v0.1.1

### Fixed

- ARM image now includes the ARM binary instead of the Intel binary.

## [0.1.0][] - 2023-07-27

Initial public release.

[0.1.0]: https://github.com/zombiezen/nixcached/releases/tag/v0.1.0
