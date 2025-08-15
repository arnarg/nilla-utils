{config}: let
  inherit (config) lib;
  inherit (builtins) readDir filter attrNames concatMap hasAttr listToAttrs replaceStrings;
in {
  config.lib.utils = {
    loadDirsCond = f: dir: let
      contents = readDir dir;
    in
      listToAttrs
      (map (
          n: {
            name = n;
            value = dir + "/${n}";
          }
        )
        (filter
          (n: contents."${n}" == "directory" && f dir n)
          (attrNames contents)));

    loadDirsWithFile = file:
      lib.utils.loadDirsCond (
        d: n: let
          contents = readDir "${d}/${n}";
        in
          (hasAttr file contents) && (contents.${file} == "regular")
      );

    loadHostsFromDir = dir: file: let
      hosts' = let
        contents = readDir dir;
      in
        filter
        (n: contents."${n}" == "directory")
        (attrNames contents);
    in
      concatMap
      (
        n: let
          contents = readDir "${dir}/${n}";
          hasConfig =
            (hasAttr file contents)
            && (contents.${file} == "regular");
        in
          if hasConfig
          then [
            {
              hostname = n;
              configuration = import "${dir}/${n}/${file}";
            }
          ]
          else []
      )
      hosts';

    # TODO
    homeListUsers = dir: let
      users' = let
        contents = readDir dir;
      in
        filter
        (n: contents."${n}" == "directory")
        (attrNames contents);
    in
      concatMap
      (
        n: let
          contents = readDir "${dir}/${n}/users";
          filenames = filter (name: contents.${name} == "regular") (builtins.attrNames contents);
          usernames = map (name: replaceStrings [".nix"] [""] name) filenames;
        in [
          {
            hostname = n;
            users = usernames;
          }
        ]
      )
      users';
  };
}
