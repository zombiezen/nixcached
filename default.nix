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

  vendorHash = "sha256-QzljuvSk2w+m33Zne0tE1qkE/q9SwksVPgyXuggnbyg=";

  meta = {
    description = "Nix cache daemon";
    homepage = "https://github.com/zombiezen/nixcached";
    license = lib.licenses.asl20;
    maintainers = [ lib.maintainers.zombiezen ];
  };
}
