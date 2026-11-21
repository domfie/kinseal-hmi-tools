# Publishing on GitHub

Suggested repository name: **kinseal-hmi-tools**

Suggested description:

```text
KinSeal Studio WinUSB bridge and HMI uploader for Windows 11 ARM, UTM on Mac, and Apple Silicon macOS. Tested with AMZ043W01, including native macOS uploads.
```

Suggested topics, entered through the gear icon next to **About**:

```text
kinseal hmi amz043w01 windows-11 windows-11-arm windows-arm arm64 utm mac macos apple-silicon winusb usb reverse-engineering golang
```

Enter topics individually. GitHub topics use lowercase letters and hyphens.

## Publish through the browser

1. Select **New repository** on GitHub, enter `kinseal-hmi-tools` and choose
   its visibility. A public repository makes the uploaded files publicly
   accessible. Create the repository.
2. Select **Uploading an existing file** or **Add file → Upload files**.
   Upload the **contents of the prepared `repository` folder**: README,
   documentation, `studio-bridge`, `uploader` and `third_party`. Do not add
   the outer folder as an extra directory level.
3. Select **Commit changes**. Add the description and topics under **About**.
4. Select **Releases → Draft a new release**, for example tag `v0.1.0` with
   title `KinSeal tools for Windows 11 ARM / UTM and Mac Apple Silicon`.
5. Attach the ZIPs from the adjacent `release-assets` folder. Use the text
   in `RELEASE_NOTES.md`, including the sudo/kernel-driver-detach requirement
   for the confirmed Mac test, then publish the release. Component versions are listed in the release
   notes and build metadata.

The MIT license for the independently written components is included in
`LICENSE`. Preserve that file, `LICENSING.md`, the separate libusb license and
third-party notices. The project does not grant rights to KinSeal Studio or
other manufacturer software, and those binaries are not included.

GitHub documentation:

- [Create a repository](https://docs.github.com/en/repositories/creating-and-managing-repositories/creating-a-new-repository)
- [Add topics](https://docs.github.com/en/repositories/managing-your-repositorys-settings-and-features/customizing-your-repository/classifying-your-repository-with-topics)
- [Create a release](https://docs.github.com/en/repositories/releasing-projects-on-github/managing-releases-in-a-repository)
