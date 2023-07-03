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

  vendorHash = "sha256-THB1wu7Jek72w5wEhG0a0cUiVXrMhzOp06lxRSGRFtY=";

  meta = {
    description = "Nix cache daemon";
    homepage = "https://github.com/zombiezen/nixcached";
    license = lib.licenses.asl20;
    maintainers = [ lib.maintainers.zombiezen ];
  };
}
