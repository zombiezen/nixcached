# Copyright 2023 Ross Light
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     https://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.
#
# SPDX-License-Identifier: Apache-2.0

{
  description = "Nix cache daemon";

  inputs = {
    nixpkgs.url = "nixpkgs";
    flake-utils.url = "flake-utils";
    flake-compat = {
      url = "github:edolstra/flake-compat";
      flake = false;
    };
  };

  outputs = { self, nixpkgs, flake-utils, ... }:
    flake-utils.lib.eachDefaultSystem (system:
      let
        pkgs = import nixpkgs { inherit system; };
      in {
        packages = {
          default = self.lib.mkNixcached pkgs;

          ci = pkgs.linkFarm "nixcached-ci" (
            [
              { name = "nixcached"; path = self.packages.${system}.default; }
            ] ++ pkgs.lib.lists.optional (self.packages.${system} ? docker-amd64) {
              name = "docker-image-nixcached-amd64.tar.gz";
              path = self.packages.${system}.docker-amd64;
            } ++ pkgs.lib.lists.optional (self.packages.${system} ? docker-arm64) {
              name = "docker-image-nixcached-arm64.tar.gz";
              path = self.packages.${system}.docker-arm64;
            }
          );
        } // pkgs.lib.optionalAttrs pkgs.hostPlatform.isLinux {
          docker-amd64 = self.lib.mkDocker {
            pkgs = (self.lib.pkgsCross system).linux-amd64;
          };
          docker-arm64 = self.lib.mkDocker {
            pkgs = (self.lib.pkgsCross system).linux-arm64;
          };

          systemd-example =
            let
              inherit (pkgs.nixos {
                imports = [ self.nixosModules.default ];
                services.nixcached.upload = {
                  enable = true;
                  bucketURL = "s3://nix-cache";
                  postBuildHook = true;
                };
                services.nixcached.serve = {
                  enable = true;
                  store = "s3://nix-cache";
                };
                system.stateVersion = "23.11";
              }) etc;
            in
              pkgs.runCommandLocal "nixcached-systemd-example" {} ''
                mkdir -p "$out/etc/nix" "$out/etc/systemd/system"
                cp --reflink=auto \
                  ${etc}/etc/nix/nix.conf \
                  "$out/etc/nix/"
                cp --reflink=auto \
                  ${etc}/etc/systemd/system/nixcached-serve.service \
                  ${etc}/etc/systemd/system/nixcached-serve.socket \
                  ${etc}/etc/systemd/system/nixcached-upload.service \
                  ${etc}/etc/systemd/system/nixcached-upload.socket \
                  "$out/etc/systemd/system/"
              '';
        };

        apps.default = {
          type = "app";
          program = "${self.packages.${system}.default}/bin/nixcached";
        };

        checks = {
          nixcached = self.packages.${system}.default;
          nixcached-version = self.packages.${system}.default.tests.version;
        };

        devShells.default = pkgs.mkShell {
          inputsFrom = [ self.packages.${system}.default.nixcached ];

          packages = [
            self.packages.${system}.default.xz

            pkgs.go-tools # staticcheck
            pkgs.gotools # stringer, etc.
          ];
        };
      }
    ) // {
      nixosModules.default = { pkgs, lib, ... }: {
        imports = [ ./module.nix ];
        services.nixcached.package = lib.mkDefault self.packages.${pkgs.hostPlatform.system}.default;
      };

      lib.pkgsCross = system: let pkgs = import nixpkgs { inherit system; }; in {
        linux-amd64 = if pkgs.hostPlatform.isLinux && pkgs.hostPlatform.isx86_64 then pkgs
          else pkgs.pkgsCross.musl64;
        linux-arm64 = if pkgs.hostPlatform.isLinux && pkgs.hostPlatform.isAarch64 then pkgs
          else pkgs.pkgsCross.aarch64-multiplatform-musl;
      };

      lib.mkNixcached = pkgs: pkgs.callPackage ./package.nix {
        git = pkgs.pkgsBuildBuild.git;
        go = pkgs.pkgsBuildHost.go_1_21;
        redo-apenwarr = pkgs.pkgsBuildBuild.redo-apenwarr;
        tailwindcss = pkgs.pkgsBuildBuild.nodePackages.tailwindcss;
      };

      lib.mkDocker =
        { pkgs
        , name ? "ghcr.io/zombiezen/nixcached"
        , tag ? null
        }:
        let
          nixcached = self.lib.mkNixcached pkgs;
        in
          (pkgs.dockerTools.buildImage {
            inherit name;
            tag = if builtins.isNull tag then nixcached.version else tag;

            copyToRoot = pkgs.buildEnv {
              name = "nixcached";
              paths = [
                nixcached
                pkgs.cacert
              ];
            };

            config = {
              Entrypoint = [ "/bin/nixcached" ];

              Labels = {
                "org.opencontainers.image.source" = "https://github.com/zombiezen/nixcached";
                "org.opencontainers.image.licenses" = "Apache-2.0";
                "org.opencontainers.image.version" = nixcached.version;
              };
            };

          }).overrideAttrs (previous: { passthru = previous.passthru // { inherit nixcached; }; });
    };
}
