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

{ name
, tag
, rev ? null
, system ? builtins.currentSystem
}:

let
  flake = builtins.getFlake ("git+file:${builtins.toString ./.}" + (if !(builtins.isNull rev) then "?rev=${rev}&shallow=1" else ""));
in
  flake.lib.mkDocker {
    pkgs = import flake.inputs.nixpkgs { inherit system; };
    inherit name tag;
  }
