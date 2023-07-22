{ lib
, stdenv
, doCheck ? false
, cacert
, git
, go
, redo-apenwarr
, sass
}:

let
  pname = "nixcached";
  version = "0.1.0";

  vendorHash = "sha256-uAzyRpoP4QJLQFTFbpLN5Q3VFUdw8yML/vuSohTdVWk=";

  src = builtins.path {
    name = "${pname}-source";
    path = ./.;
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
in

stdenv.mkDerivation {
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

  configurePhase = ''
    export GOCACHE=$TMPDIR/go-cache
    export GOPATH="$TMPDIR/go"
    export GOSUMDB=off
    export GOPROXY=off

    rm -rf vendor
    cp -r --reflink=auto ${go-modules} vendor
  '';
  buildPhase = ''
    redo -j$NIX_BUILD_CORES
  '';
  installPhase = ''
    mkdir -p "$out/bin"
    cp nixcached "$out/bin"
  '';
  checkPhase = ''
    redo -j$NIX_BUILD_CORES test
  '';

  passthru = {
    inherit go go-modules sass vendorHash;
    redo = redo-apenwarr;
  };

  meta = {
    description = "Nix cache daemon";
    homepage = "https://github.com/zombiezen/nixcached";
    license = lib.licenses.asl20;
    maintainers = [ lib.maintainers.zombiezen ];
  };
}
