{ lib
, buildGoModule
}:

buildGoModule {
  pname = "nixcached";
  version = "0.1.0";

  src = builtins.path {
    name = "nixcached-source";
    path = ./.;
  };

  vendorHash = "sha256-3t1LNekoa9bENHq18Ww80VXHecTB75i0cxrFzyWvgKg=";

  meta = {
    description = "Nix cache daemon";
    homepage = "https://github.com/zombiezen/nixcached";
    license = lib.licenses.asl20;
    maintainers = [ lib.maintainers.zombiezen ];
  };
}
