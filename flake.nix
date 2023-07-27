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

          systemd-example =
            let
              inherit (pkgs.nixos {
                imports = [ self.nixosModules.default ];
                services.nixcached.upload = {
                  enable = true;
                  bucketURL = "s3://nix-cache";
                };
                system.stateVersion = "23.11";
              }) etc;
            in
              pkgs.runCommandLocal "nixcached-systemd-example" {} ''
                mkdir -p "$out/etc/systemd/system"
                cp --reflink=auto \
                  ${etc}/etc/systemd/system/nixcached-upload.service \
                  ${etc}/etc/systemd/system/nixcached-upload.socket \
                  "$out/etc/systemd/system/"
              '';

          ci = pkgs.linkFarm "nixcached-ci" (
            [
              { name = "nixcached"; path = self.packages.${system}.default; }
              { name = "nixcached-tests/version"; path = self.packages.${system}.default.tests.version; }
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
            pkgs = if pkgs.hostPlatform.isx86_64 then pkgs else pkgs.pkgsCross.musl64;
          };
          docker-arm64 = self.lib.mkDocker {
            pkgs = if pkgs.hostPlatform.isAarch64 then pkgs else pkgs.pkgsCross.aarch64-multiplatform-musl;
          };
        };

        apps.default = {
          type = "app";
          program = "${self.packages.${system}.default}/bin/nixcached";
        };

        devShells.default = pkgs.mkShell {
          packages = [
            self.packages.${system}.default.go
            self.packages.${system}.default.redo
            self.packages.${system}.default.sass
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

      lib.mkNixcached = pkgs: pkgs.callPackage ./package.nix {
        go = pkgs.go_1_20;
        sass = pkgs.dart-sass;
      };

      lib.mkDocker =
        { pkgs
        , name ? "ghcr.io/zombiezen/nixcached"
        , tag ? null
        }:
        let
          nixcached = self.lib.mkNixcached pkgs;
        in
          pkgs.dockerTools.buildImage {
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
          };
    };
}
