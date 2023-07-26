{ pkgs, lib, config, ... }:

{
  options.services.nixcached = let inherit (lib) mkOption; in {
    package = mkOption {
      type = lib.types.package;
      description = lib.mdDoc "The nixcached package to use.";
    };

    upload = {
      enable = lib.mkEnableOption (lib.mdDoc "nixcached upload");
      bucketURL = mkOption {
        type = lib.types.str;
        example = lib.literalExpression "\"s3://example-bucket\"";
        description = lib.mdDoc "Bucket to upload artifacts to.";
      };
      socketUser = mkOption {
        type = lib.types.str;
        default = "root";
        description = lib.mdDoc "Name of user to own socket.";
      };
      socketGroup = mkOption {
        type = lib.types.nullOr lib.types.str;
        default = null;
        defaultText = lib.literalMD "Group of `socketUser`.";
        description = lib.mdDoc "Name of group to own socket.";
      };
      socketMode = mkOption {
        type = lib.types.strMatching "[0-7]{1,5}";
        default = "0666";
        description = lib.mdDoc "Permissions of the socket.";
      };
    };
  };

  config = let cfg = config.services.nixcached; in lib.mkMerge [
    (lib.mkIf cfg.upload.enable {
      systemd.sockets.nixcached-upload = {
        socketConfig.ListenFIFO = "/run/nixcached-upload";
        socketConfig.SocketUser = cfg.upload.socketUser;
        socketConfig.SocketGroup = lib.mkIf (!builtins.isNull cfg.upload.socketGroup) cfg.upload.socketGroup;
        socketConfig.SocketMode = cfg.upload.socketMode;
      };

      systemd.services.nixcached-upload = {
        description = "nixcached upload";

        requires = [
          "local-fs.target"
          "nixcache-upload.socket"
        ];
        wants = [
          "network-online.target"
        ];
        after = [
          "local-fs.target"
          "network-online.target"
          "nixcache-upload.socket"
        ];

        serviceConfig.StandardInput = "socket";
        environment.SSL_CERT_FILE = "${pkgs.cacert}/etc/ssl/certs/ca-bundle.crt";

        path = [ cfg.package ];
        script = ''
          exec nixcached upload --input ${cfg.upload.bucketURL}
        '';
      };

      # Install nixcached globally so users can run `nixcached send`.
      environment.systemPackages = [ cfg.package ];
    })
  ];
}
