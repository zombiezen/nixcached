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
      postBuildHook = mkOption {
        type = lib.types.bool;
        default = false;
        example = true;
        description = lib.mdDoc "Whether to send Nix-built store paths to this service.";
      };
    };

    serve = {
      enable = lib.mkEnableOption (lib.mdDoc "nixcached serve");
      store = mkOption {
        type = lib.types.str;
        example = lib.literalExpression "\"s3://example-bucket\"";
        description = lib.mdDoc "URL of Nix store to proxy.";
      };
      port = mkOption {
        type = lib.types.port;
        default = 38380;
        description = lib.mdDoc "Port to serve HTTP on.";
      };
      useAsSubstituter = mkOption {
        type = lib.types.bool;
        default = true;
        example = false;
        description = lib.mdDoc "Whether to use the server as a substituter for the system.";
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
          "nixcached-upload.socket"
        ];
        wants = [
          "network-online.target"
        ];
        after = [
          "local-fs.target"
          "network-online.target"
          "nixcached-upload.socket"
        ];

        serviceConfig.StandardInput = "socket";
        serviceConfig.KillMode = "mixed";
        serviceConfig.KillSignal = "SIGHUP";
        environment.SSL_CERT_FILE = "${pkgs.cacert}/etc/ssl/certs/ca-bundle.crt";

        path = [ cfg.package ];
        script = ''
          exec nixcached upload ${lib.strings.escapeShellArg cfg.upload.bucketURL}
        '';
      };

      # Install nixcached globally so users can run `nixcached send`.
      environment.systemPackages = [ cfg.package ];
    })

    (lib.mkIf (cfg.upload.enable && cfg.upload.postBuildHook) {
      nix.extraOptions = let
        hook = pkgs.writeShellScriptBin "nixcached-post-build-hook" ''
          ${cfg.package}/bin/nixcached send -o /run/nixcached-upload $OUT_PATHS
        '';
      in ''
        post-build-hook = ${hook}/bin/nixcached-post-build-hook
      '';
    })

    (lib.mkIf cfg.serve.enable {
      systemd.sockets.nixcached-serve = {
        description = "nixcached serve HTTP";
        listenStreams = [ "127.0.0.1:${builtins.toString cfg.serve.port}" ];
        wantedBy = [ "sockets.target" ];
      };

      systemd.services.nixcached-serve = {
        description = "nixcached serve";

        requires = [
          "nixcached-serve.socket"
        ];
        wants = [
          "local-fs.target"
          "network-online.target"
        ];
        after = [
          "local-fs.target"
          "network-online.target"
          "nixcached-serve.socket"
        ];

        serviceConfig.Sockets = [ "nixcached-serve.socket" ];

        environment.SSL_CERT_FILE = "${pkgs.cacert}/etc/ssl/certs/ca-bundle.crt";

        path = [ cfg.package ];
        script = ''
          exec nixcached serve --systemd ${cfg.serve.store}
        '';
      };
    })

    (lib.mkIf (cfg.serve.enable && cfg.serve.useAsSubstituter) {
      nix.settings.substituters = [
        "http://localhost:${builtins.toString cfg.serve.port}"
      ];
    })
  ];
}
