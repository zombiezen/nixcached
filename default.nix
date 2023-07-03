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

  vendorHash = "sha256-Jn2d+yCvzRnCBIdjr2YifJd6JdzrdkBCJhC+/NOR8uI=";

  meta = {
    description = "Nix cache daemon";
    homepage = "https://github.com/zombiezen/nixcached";
    license = lib.licenses.asl20;
    maintainers = [ lib.maintainers.zombiezen ];
  };
}
