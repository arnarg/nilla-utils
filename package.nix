{
  lib,
  buildGoApplication,
}:
let
  version = "0.0.0-alpha.20";
in
buildGoApplication {
  inherit version;
  pname = "nilla-utils-plugins";

  src = builtins.filterSource (
    path: type:
    type == "directory"
    || baseNameOf path == "go.mod"
    || baseNameOf path == "go.sum"
    || lib.hasSuffix ".go" path
  ) ./.;

  modules = ./gomod2nix.toml;

  subPackages = [
    "cmd/nilla-os"
    "cmd/nilla-home"
    "cmd/nilla-microvm"
  ];

  ldflags = [ "-X main.version=${version}" ];
}
