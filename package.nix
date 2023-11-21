{ lib
, stdenv
, doCheck ? false
, makeBinaryWrapper
, nix-gitignore
, runCommandLocal
, testers
, cacert
, git
, go
, redo-apenwarr
, tailwindcss
, xz
}:

let
  pname = "nixcached";
  version = "0.3.0";

  vendorHash = "sha256-p7iZ9FrMXzI+Ik4coy+i61nqKRiZT44oDJ+E0DDdiFk=";

  passthru = {
    inherit go go-modules tailwindcss vendorHash xz;
    redo = redo-apenwarr;

    tests.version = testers.testVersion {
      package = nixcached;
      inherit version;
    };
  };

  meta = {
    description = "Nix cache daemon";
    homepage = "https://github.com/zombiezen/nixcached";
    license = lib.licenses.asl20;
    maintainers = [ lib.maintainers.zombiezen ];
  };

  src = let
    root = ./.;
    patterns = nix-gitignore.withGitignoreFile extraIgnores root;
    extraIgnores = [ ".github" ".vscode" "*.nix" "flake.lock" ];
  in builtins.path {
    name = "${pname}-source";
    path = root;
    filter = nix-gitignore.gitignoreFilterPure (_: _: true) patterns root;
  };

  go-modules = stdenv.mkDerivation {
    name = "${pname}-go-modules";
    inherit src;

    inherit (go) GOOS GOARCH CGO_ENABLED;
    GO111MODULE = "on";

    impureEnvVars = lib.fetchers.proxyImpureEnvVars ++ [
      "GIT_PROXY_COMMAND" "SOCKS_SERVER" "GOPROXY"
    ];

    nativeBuildInputs = [
      cacert
      git
      go
    ];

    configurePhase = ''
      export GOCACHE=$TMPDIR/go-cache
      export GOPATH="$TMPDIR/go"
    '';
    buildPhase = ''
      go mod vendor
      mkdir -p vendor
    '';
    installPhase = ''
      cp -r --reflink=auto vendor $out
    '';

    outputHashMode = "recursive";
    outputHash = vendorHash;
    dontFixup = true;
  };

  nixcached = stdenv.mkDerivation {
    inherit pname version src;

    nativeBuildInputs = [
      go
      redo-apenwarr
      tailwindcss
    ];

    inherit (go) GOOS GOARCH CGO_ENABLED;
    GO111MODULE = "on";
    GOFLAGS = [ "-mod=vendor" "-trimpath" ];

    inherit doCheck;

    patchPhase = ''
      runHook prePatch

      echo "$version" > version.txt

      runHook postPatch
    '';

    configurePhase = ''
      runHook preConfigure

      export GOCACHE=$TMPDIR/go-cache
      export GOPATH="$TMPDIR/go"
      export GOSUMDB=off
      export GOPROXY=off

      rm -rf vendor
      cp -r --reflink=auto ${go-modules} vendor

      runHook postConfigure
    '';
    buildPhase = ''
      runHook preBuild

      redo -j$NIX_BUILD_CORES nixcached

      runHook postBuild
    '';
    installPhase = ''
      runHook preInstall

      mkdir -p "$out/bin"
      cp nixcached "$out/bin"

      runHook postInstall
    '';
    checkPhase = ''
      runHook preCheck

      redo -j$NIX_BUILD_CORES test

      runHook postCheck
    '';

    inherit passthru meta;
  };
in

runCommandLocal "${pname}-${version}" {
  inherit pname version;

  nativeBuildInputs = [
    makeBinaryWrapper
  ];

  passthru = passthru // {
    inherit nixcached;
  };

  inherit meta;
} ''
  mkdir -p "$out/bin"
  makeWrapper "${nixcached}/bin/nixcached" "$out/bin/nixcached" \
    --set XZ "${xz}/bin/xz"
''
