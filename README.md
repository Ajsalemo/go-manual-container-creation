# go-manual-container-creation
A small application that programmatically creates a container without a container runtime:
- Downloads Alpine mini rootfs
  - Untar's it 
- Creates the rootfs
- Sets up a working directory
- Sets up namespaces
- Sets up a random hostname
- Mounts /proc
- Uses a nonroot user