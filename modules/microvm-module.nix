{
  pkgs,
  lib,
  config,
  ...
}:
let
  cfg = config.utils.microvm;
  stateDir = "/var/lib/microvms";

  shareEntry = lib.types.submodule {
    options = {
      source = lib.mkOption {
        type = lib.types.str;
        description = ''
          The source directory path on the host to share with the VM.
        '';
      };
      mountPoint = lib.mkOption {
        type = lib.types.str;
        description = ''
          The mount point path inside the VM where the share will be mounted.
        '';
      };
    };
  };
in
{
  options = with lib; {
    utils.microvm = {
      vmName = mkOption {
        type = types.str;
        internal = true;
      };
      manageHostKeys = mkOption {
        type = types.bool;
        default = true;
        description = ''
          Instruct nilla-utils to automatically generate SSH host keys and mount them in the MicroVM.
        '';
      };
      shares = {
        directories = mkOption {
          type =
            with types;
            listOf (
              coercedTo str (path: {
                source = path;
                mountPoint = path;
              }) shareEntry
            );
          default = [ ];
          description = ''
            List of directories that should be mounted into the VM.
            Each entry can be either a path string (mounted at the same path in the VM)
            or an attribute set with `source` (host path) and `mountPoint` (VM path).
          '';
        };
        managePermissions = mkOption {
          type = types.bool;
          default = true;
          description = ''
            If set to true, it will make sure correct ownership is set if paths begin with `/home/<user>`.
          '';
        };
      };
    };
    microvm.activationPackage = mkOption {
      type = types.package;
      internal = true;
    };
  };

  config = {
    # Higher priority than `mkOptionDefault` but lower than `mkDefault`.
    networking.hostName = lib.mkOverride 1400 cfg.vmName;

    # To ensure permissions when mounting managed shares.
    systemd.tmpfiles.rules =
      let
        # Regex to match /home/<user>/ or /home/<user> and capture the username
        homeUserRegex = "/home/([^/]+)/?.*";

        # Get user/group info for a share path, returns null if path doesn't match or user doesn't exist
        getUserInfo =
          share:
          let
            match = builtins.match homeUserRegex share;
            user = lib.head match;
            userExists = builtins.hasAttr user config.users.users;
            group = if userExists then (config.users.users.${user}.group or user) else user;
          in
          if match == null || !userExists then null else { inherit user group; };

        # Generate tmpfile entries for a share path
        makeEntries =
          share:
          let
            info = getUserInfo share.mountPoint;
          in
          if info == null then
            [ ]
          else
            lib.pipe share.mountPoint [
              (lib.removePrefix "/home/${info.user}")
              (lib.removePrefix "/")
              (lib.splitString "/")
              lib.init
              (
                parts:
                let
                  len = builtins.length parts;
                  base = "/home/${info.user}/";
                in
                lib.genList (i: {
                  path = "${base}" + (lib.concatStringsSep "/" (lib.take (i + 1) parts));
                  inherit (info) user group;
                }) len
              )
            ];

        entries = lib.concatMap makeEntries cfg.shares.directories;
      in
      lib.optionals cfg.shares.managePermissions (
        map (e: "d ${e.path} 0750 ${e.user} ${e.group} - -") (lib.unique entries)
      );

    # Use SSH host keys mounted from outside the VM.
    services.openssh.hostKeys = lib.optional cfg.manageHostKeys {
      path = "/etc/ssh/host-keys/ssh_host_ed25519_key";
      type = "ed25519";
    };

    microvm.shares =
      # Create virtiofs share for every share in utils.microvm config.
      (map (share: {
        proto = "virtiofs";
        tag = lib.last (lib.splitString "/" share.source);
        source = share.source;
        mountPoint = share.mountPoint;
      }) cfg.shares.directories)
      # Mount SSH host keys for this MicroVM from the host.
      ++ (lib.optional cfg.manageHostKeys {
        proto = "virtiofs";
        tag = "ssh-keys";
        source = "${stateDir}/${cfg.vmName}/ssh-host-keys";
        mountPoint = "/etc/ssh/host-keys";
      });

    # Build the activation package
    microvm.activationPackage =
      let
        colors = {
          normal = "\\033[0m";
          red = "\\033[0;31m";
          green = "\\033[0;32m";
          boldRed = "\\033[1;31m";
          boldYellow = "\\033[1;33m";
          boldGreen = "\\033[1;32m";
          boldCyan = "\\033[1;36m";
        };

        uninstallVM = pkgs.writeShellScript "uninstall-script-vm-${cfg.vmName}" ''
          set -e

          STATE_DIR="${stateDir}"
          VM_NAME="${cfg.vmName}"
          DIR="$STATE_DIR/$VM_NAME"

          if [ ! -e "$DIR" ]; then
            echo "Error: MicroVM $VM_NAME is not installed."
            exit 1
          fi

          if systemctl is-active -q "microvm@$VM_NAME.service"; then
            echo "Stopping MicroVM $VM_NAME..."
            systemctl stop "microvm@$VM_NAME.service"
          fi

          rm -f "/nix/var/nix/gcroots/microvm/$VM_NAME"
          rm -f "/nix/var/nix/gcroots/microvm/booted-$VM_NAME"

          rm -rf "$DIR"

          echo "Uninstalled MicroVM $VM_NAME."
        '';

        manageVM = pkgs.writeShellScript "manage-vm-${cfg.vmName}" ''
          set -e

          STATE_DIR="${stateDir}"
          VM_NAME="${cfg.vmName}"
          DIR="$STATE_DIR/$VM_NAME"
          DECLARED_RUNNER="${config.microvm.declaredRunner}"

          generate_ssh_keys() {
            local keys_dir="$DIR/ssh-host-keys"

            if [ ! -d "$keys_dir" ]; then
              mkdir -p "$keys_dir"
              chmod 755 "$keys_dir"
            fi

            if [ ! -f "$keys_dir/ssh_host_ed25519_key" ]; then
              echo "Generating SSH host keys for MicroVM."
              ${pkgs.openssh}/bin/ssh-keygen  -t ed25519 \
                                              -f "$keys_dir/ssh_host_ed25519_key" \
                                              -N "" \
                                              -C "${config.networking.hostName}" >/dev/null
              chmod 600 "$keys_dir/ssh_host_ed25519_key"
              chmod 644 "$keys_dir/ssh_host_ed25519_key.pub"
            fi
          }

          setup_gcroots() {
            mkdir -p /nix/var/nix/gcroots/microvm
            rm -f "/nix/var/nix/gcroots/microvm/$VM_NAME"
            ln -s "$DIR/current" "/nix/var/nix/gcroots/microvm/$VM_NAME"
            rm -f "/nix/var/nix/gcroots/microvm/booted-$VM_NAME"
            ln -s "$DIR/booted" "/nix/var/nix/gcroots/microvm/booted-$VM_NAME"
          }

          cmd_install() {
            if [ -e "$DIR" ]; then
              echo "Error: $DIR already exists."
              exit 1
            fi

            mkdir -p "$DIR"

            ln -sf "$DECLARED_RUNNER" "$DIR/current"

            chown :kvm -R "$DIR"
            chmod -R a+rX "$DIR"
            chmod g+w "$DIR"

            ln -sf "${uninstallVM}" "$DIR/uninstall"

            setup_gcroots


            ${lib.optionalString cfg.manageHostKeys ''
              generate_ssh_keys
            ''}

            echo -e "${colors.green}Installed MicroVM $VM_NAME.${colors.normal} Start with: ${colors.boldCyan}systemctl start microvm@$VM_NAME.service${colors.normal}"
          }

          cmd_update() {
            if [ ! -e "$DIR" ]; then
              echo "Error: MicroVM $VM_NAME is not installed. Run 'install' first."
              exit 1
            fi

            local OLD=""
            [ -L "$DIR/current" ] && OLD=$(readlink "$DIR/current")

            # Replace current symlink
            ln -s "$DECLARED_RUNNER" "$DIR/new-current"
            mv -T "$DIR/new-current" "$DIR/current"

            # Replace uninstall script
            ln -s "${uninstallVM}" "$DIR/new-uninstall"
            mv -T "$DIR/new-uninstall" "$DIR/uninstall"

            ${lib.optionalString cfg.manageHostKeys ''
              generate_ssh_keys
            ''}

            local RESTART=n
            while [ $# -gt 0 ]; do
              case "$1" in
                --restart) RESTART=y ;;
                *) echo "Unknown option: $1"; exit 1 ;;
              esac
              shift
            done

            if systemctl is-active -q "microvm@$VM_NAME.service"; then
              if [ "$OLD" != "$DECLARED_RUNNER" ]; then
                if [ "$RESTART" = y ]; then
                  echo "Restarting MicroVM $VM_NAME..."
                  systemctl restart "microvm@$VM_NAME.service"
                else
                  echo "MicroVM $VM_NAME updated. Restart to apply changes: systemctl restart microvm@$VM_NAME.service"
                fi
              else
                echo "MicroVM $VM_NAME is up to date."
              fi
            else
              echo "MicroVM $VM_NAME updated (not running)."
            fi
          }

          case "''${1:-}" in
            install)
              cmd_install
              ;;
            update)
              shift
              cmd_update "$@"
              ;;
            *)
              echo "Usage: $0 {install|update [--restart]}"
              exit 1
              ;;
          esac
        '';
      in
      pkgs.linkFarm "activation-package-${cfg.vmName}" [
        {
          name = "bin/manage-vm";
          path = manageVM;
        }
        {
          name = "declared-runner";
          path = config.microvm.declaredRunner;
        }
      ];
  };
}
