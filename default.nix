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

  vendorHash = "sha256-in2v3GLG9gZRh4E8NmlOY31OWdYA3SaFmL8m/wLRUYA=";

  meta = {
    description = "Nix cache daemon";
    homepage = "https://github.com/zombiezen/nixcached";
    license = lib.licenses.asl20;
    maintainers = [ lib.maintainers.zombiezen ];
  };
}
