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
  };

  outputs = { self, nixpkgs, flake-utils, ... }:
    flake-utils.lib.eachDefaultSystem (system:
      let
        pkgs = import nixpkgs { inherit system; };
      in {
        packages.default = pkgs.callPackage ./. {
          go = pkgs.go_1_20;
          sass = pkgs.dart-sass;
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

            pkgs.go-tools # staticcheck
            pkgs.gotools # stringer, etc.
          ];
        };
      }
    );
}
