# nixcached Release Notes

The format is based on [Keep a Changelog][],
and this project adheres to [Semantic Versioning][].

[Keep a Changelog]: https://keepachangelog.com/en/1.0.0/
[Semantic Versioning]: https://semver.org/spec/v2.0.0.html
[Unreleased]: https://github.com/zombiezen/nixcached/compare/v0.3.1...HEAD

## [0.3.1][] - 2023-11-21

Version 0.3.1 fixes several show-stopping issues.

[0.3.1]: https://github.com/zombiezen/nixcached/releases/tag/v0.3.1

### Fixed

- Uploaded `.narinfo` files have the correct compression set
  when a non-bzip2 algorithm is used.
- `serve` no longer 404s on NAR files at the top of a bucket.

## [0.3.0][] - 2023-11-21

Version 0.3 fixes the graceful shutdown mechanism for `upload`.

[0.3.0]: https://github.com/zombiezen/nixcached/releases/tag/v0.3.0

### Added

- `send --finish` is a new option that signals for `upload` to exit
  after receiving its arguments.
  `upload` must be run with `--allow-finish` to support this.

### Removed

- `SIGHUP` is no longer specially handled by `upload`.

### Fixed

- The services in the NixOS modules now have `Restart=` set correctly.

## [0.2.0][] - 2023-11-02

Version 0.2 includes many enhancements to the `serve` command,
as well as some quality-of-life improvements to the `upload` command.

[0.2.0]: https://github.com/zombiezen/nixcached/releases/tag/v0.2.0

### Added

- Uploads are now performed concurrently, defaulting to two at a time.
  The number can be tuned with the `upload --jobs` flag.
- The NixOS module now includes a `services.nixcached.serve` option set
  that configures a `nixcached serve` systemd service.
  This service can optionally be used as a system-wide substituter.
- The NixOS module includes a `services.nixcached.upload.postBuildHook` option
  that provides a minimal shell script to trigger uploads as a
  [Nix post-build hook](https://nixos.org/manual/nix/stable/advanced-topics/post-build-hook.html).
- `serve --systemd` allows the server to be run with
  [systemd socket activation](https://0pointer.de/blog/projects/socket-activation.html).
- `serve` can now handle `ssh://` URLs.

### Changed

- Sending `SIGHUP` to `upload` now acts as a graceful shutdown,
  even when `--keep-alive` is in effect.

### Fixed

- The dependencies in the `nixcached-upload.service` from the NixOS module
  are now correctly specified.

## [0.1.1][] - 2023-07-27

Version 0.1.1 has no changes from 0.1.0
but fixes the 64-bit ARM Docker image.

[0.1.1]: https://github.com/zombiezen/nixcached/releases/tag/v0.1.1

### Fixed

- ARM image now includes the ARM binary instead of the Intel binary.

## [0.1.0][] - 2023-07-27

Initial public release.

[0.1.0]: https://github.com/zombiezen/nixcached/releases/tag/v0.1.0
