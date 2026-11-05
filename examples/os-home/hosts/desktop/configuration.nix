{
  boot.loader.grub.devices = [ "/dev/sda" ];
  fileSystems."/".device = "/dev/sda1";
  system.stateVersion = "24.11";

  # home-manager reads the home directory from here.
  users.users.adam.isNormalUser = true;
}
