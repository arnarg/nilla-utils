{
  lib,
  buildGoApplication,
  nvd,
  makeWrapper,
}: let
  version = "0.0.0-alpha.8";
in
  buildGoApplication {
    inherit version;
    pname = "nilla-utils-plugins";

    src =
      builtins.filterSource (
        path: type:
          type
          == "directory"
          || baseNameOf path == "go.mod"
          || baseNameOf path == "go.sum"
          || lib.hasSuffix ".go" path
      )
      ./.;

    modules = ./gomod2nix.toml;

    subPackages = ["cmd/nilla-os" "cmd/nilla-home"];
    ldflags = ["-X main.version=${version}"];

    nativeBuildInputs = [makeWrapper];

    postInstall = ''
      wrapProgram $out/bin/nilla-os --prefix PATH : ${lib.makeBinPath [nvd]}
      wrapProgram $out/bin/nilla-home --prefix PATH : ${lib.makeBinPath [nvd]}
    '';
  }
