{ config }:
let
  inherit (config) lib;
  inherit (builtins)
    foldl'
    readDir
    filter
    attrNames
    concatMap
    hasAttr
    listToAttrs
    ;
in
{
  config.lib.utils = {
    loadDirsCond =
      f: dir:
      let
        contents = readDir dir;
      in
      listToAttrs (
        map (n: {
          name = n;
          value = dir + "/${n}";
        }) (filter (n: contents."${n}" == "directory" && f dir n) (attrNames contents))
      );

    loadDirsWithFile =
      file:
      lib.utils.loadDirsCond (
        d: n:
        let
          contents = readDir "${d}/${n}";
        in
        (hasAttr file contents) && (contents.${file} == "regular")
      );

    loadDirsCondRecursive =
      f: dir: moduleName: baseDir:
      let
        contents = readDir dir;

        hasFile = f dir moduleName;

        currentResult = if hasFile then { ${moduleName} = dir; } else { };

        subdirs = filter (n: contents."${n}" == "directory") (attrNames contents);

        recursiveResults = foldl' (
          acc: subdir: acc // (lib.utils.loadDirsCondRecursive f (dir + "/${subdir}") subdir baseDir)
        ) { } subdirs;
      in
      currentResult // recursiveResults;

    loadDirsWithFileRecursive =
      file: baseDir:
      let
        checkDir =
          d: n:
          let
            contents = readDir "${d}";
          in
          (hasAttr file contents) && (contents.${file} == "regular");
      in
      lib.utils.loadDirsCondRecursive checkDir baseDir "" "";

    loadHostsFromDir =
      dir: file:
      let
        hosts' =
          let
            contents = readDir dir;
          in
          filter (n: contents."${n}" == "directory") (attrNames contents);
      in
      concatMap (
        n:
        let
          contents = readDir "${dir}/${n}";
          hasConfig = (hasAttr file contents) && (contents.${file} == "regular");
        in
        if hasConfig then
          [
            {
              hostname = n;
              configuration = import "${dir}/${n}/${file}";
            }
          ]
        else
          [ ]
      ) hosts';
  };
}
