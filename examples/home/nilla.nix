let
  pins = import ../../npins;

  nilla = import pins.nilla;
in
  nilla.create ({config}: {
    includes = [
      ../../modules
    ];

    config = {
      inputs = {
        nixpkgs.src = builtins.fetchTarball {
          url = "https://releases.nixos.org/nixos/24.11/nixos-24.11.716868.60e405b241ed/nixexprs.tar.xz";
          sha256 = "111zrdbnd2b7w64q07773ksf4gfbm4gq7riggxld1gmxpimprj0j";
        };
        home-manager.src = builtins.fetchTarball {
          url = "https://github.com/nix-community/home-manager/archive/b4e98224ad1336751a2ac7493967a4c9f6d9cb3f.tar.gz";
          sha256 = "0qk1qn04willw5qrzfjs9b7815np8mr6ci68a2787g3q7444bdxp";
        };
        npkg-unstable.src = builtins.fetchTarball {
          url = "https://github.com/nixos/nixpkgs/archive/2631b0b7abcea6e640ce31cd78ea58910d31e650.tar.gz";
          sha256 = "0crx0vfmvxxzj8viqpky4k8k7f744dsqnn1ki5qj270bx2w9ssid";
        };
        hm-master.src = builtins.fetchTarball {
          url = "https://github.com/nix-community/home-manager/archive/72526a5f7cde2ef9075637802a1e2a8d2d658f70.tar.gz";
          sha256 = "0kr9ckh1bhaimifsxim3h7wn9i14d24wdhyvfwidhmzndlyjr690";
        };
      };

      systems.home."user@mysystem" = {
        system = "x86_64-linux";

        modules = [
          {
            home.username = "user";
            home.homeDirectory = "/home/user";
            home.stateVersion = "24.11";
          }
        ];
      };

      systems.home."user@unstable" = {
        system = "x86_64-linux";

        pkgs = config.inputs.npkg-unstable.result.x86_64-linux;
        home-manager = config.inputs.hm-master;

        modules = [
          ({homeModules, ...}: {
            imports = [homeModules.common];

            home.username = "user";
            home.homeDirectory = "/home/user";
            home.stateVersion = "24.11";
          })
        ];
      };

      modules.home.common = {
        programs.git.enable = true;
      };

      generators.home.folder = ./hosts;
    };
  })
