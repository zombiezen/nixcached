# nixcached: A Nix Cache Daemon

nixcached is a CLI that provides both a caching HTTP proxy
and an asynchronous build artifact upload pipeline for [Nix][].
It can use the local filesystem, [Amazon S3][], [Google Cloud Storage][],
or any S3-compatible storage like [MinIO][].

[Amazon S3]: https://aws.amazon.com/s3/
[Google Cloud Storage]: https://cloud.google.com/storage
[MinIO]: https://min.io/
[Nix]: https://nixos.org/

## Installation

nixcached is distributed as a [Docker][] image:

```shell
docker pull ghcr.io/zombiezen/nixcached
```

Alternatively, if you're already using Nix,
you can install the binary by checking out the repository
and running the following:

```shell
nix-env --file . --install
```

Or if you're using Nix Flakes:

```shell
nix profile install github:zombiezen/nixcached/v0.1.1
```

Otherwise, you can download a static binary from the [latest release][].

[Docker]: https://www.docker.com/
[latest release]: https://github.com/zombiezen/nixcached/releases/latest

## Caching Proxy

nixcached can be run as an HTTP proxy for another Nix store.
This has a number of benefits:

- Reduces the traffic to the Nix store.
- Opens an interactive explorer UI over your Nix store.
- Allows privilege/credential separation from the Nix build process.
- Ability to use native Google Cloud credentials for Google Cloud Storage.

To run nixcached as an HTTP proxy:

```shell
# Local Nix store
nixcached serve --port=8080 /nix/store
# Local folder
mkdir "$HOME/foo" && nixcached serve --port=8080 "file://$HOME/foo"
# Amazon S3
nixcached serve --port=8080 s3://nix-cache
# S3-compatible Storage
nixcached serve --port=8080 s3://mybucket?endpoint=minio.example.com:8080&disableSSL=true&s3ForcePathStyle=true
# Google Cloud Storage (GCS)
nixcached serve --port=8080 gs://nix-cache
# Remote Nix store
nixcached serve --port=8080 ssh://example.com
```

## Upload Pipeline

nixcached can be run as an upload pipeline.
This is useful for uploading build artifacts as a Nix [post-build hook][].
To do this, run `nixcached upload`
(either manually or using a service manager like systemd)
with a named pipe as input:

```shell
mkfifo /run/nixcached-upload &&
nixcached upload -k --input=/run/nixcached-upload s3://${MY_BUCKET?}
```

Then in your post-build hook run `nixcached send`:

```shell
nixcached send -o /run/nixcached-upload $OUT_PATHS
```

Sending `SIGHUP` to `nixcached upload`
will prevent any new paths from being received
and cause the process to exit after it finishes copying any Nix store objects.

[post-build hook]: https://nixos.org/manual/nix/stable/advanced-topics/post-build-hook.html

## License

[Apache 2.0](LICENSE)
