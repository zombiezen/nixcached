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
, sass
, xz
}:

let
  pname = "nixcached";
  version = "0.1.0";

  vendorHash = "sha256-13ZDO4rXYgTRsyXda9RpHvDVXpfNc6UArXznLOh78mE=";

  passthru = {
    inherit go go-modules sass vendorHash xz;
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

    inherit (go) GOOS GOARCH;
    GO111MODULE = "on";

    impureEnvVars = lib.fetchers.proxyImpureEnvVars ++ [
      "GIT_PROXY_COMMAND" "SOCKS_SERVER"
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
      sass
    ];

    inherit (go) GOOS GOARCH;
    GO111MODULE = "on";
    GOFLAGS = [ "-mod=vendor" "-trimpath" ];

    inherit doCheck;

    patchPhase = ''
      runHook prePatch

      echo "$version" > version.txt

      runHook postPatch
    '';

    configurePhase = ''
      export GOCACHE=$TMPDIR/go-cache
      export GOPATH="$TMPDIR/go"
      export GOSUMDB=off
      export GOPROXY=off

      rm -rf vendor
      cp -r --reflink=auto ${go-modules} vendor
    '';
    buildPhase = ''
      redo -j$NIX_BUILD_CORES nixcached
    '';
    installPhase = ''
      mkdir -p "$out/bin"
      cp nixcached "$out/bin"
    '';
    checkPhase = ''
      redo -j$NIX_BUILD_CORES test
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
