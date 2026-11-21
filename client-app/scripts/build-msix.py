#!/usr/bin/env python3
"""
Build an unsigned Microsoft Store MSIX package for OpenTermius (x64 Desktop Bridge / runFullTrust).
Microsoft Store signs the uploaded .msix automatically during certification.

Usage:
  python3 client-app/scripts/build-msix.py
"""
import base64
import hashlib
import json
import os
import sys
import zipfile
from pathlib import Path
from PIL import Image

ROOT = Path(__file__).resolve().parent.parent
TAURI_CONF = ROOT / "src-tauri" / "tauri.conf.json"
EXE_SRC = ROOT / "src-tauri" / "target" / "x86_64-pc-windows-msvc" / "release" / "app.exe"
ICONS_DIR = ROOT / "src-tauri" / "icons"
OUT_DIR = ROOT / "src-tauri" / "target" / "x86_64-pc-windows-msvc" / "release" / "bundle" / "msix"

IDENTITY_NAME = os.environ.get("MSIX_IDENTITY_NAME", "Primaelektrindo.OpenTermius")
PUBLISHER = os.environ.get("MSIX_PUBLISHER", "CN=4A2D9D84-E8BB-4CC3-B783-EB8AED230E75")
PUBLISHER_DISPLAY = os.environ.get("MSIX_PUBLISHER_DISPLAY", "Open Foundation")
DISPLAY_NAME = "OpenTermius"

BLOCK_SIZE = 65536


def ensure_wide_logo(dest: Path) -> None:
    icon = Image.open(ICONS_DIR / "icon.png").convert("RGBA")
    canvas = Image.new("RGBA", (310, 150), (10, 10, 15, 255))
    icon_resized = icon.resize((112, 112), Image.Resampling.LANCZOS)
    canvas.alpha_composite(icon_resized, ((310 - 112) // 2, (150 - 112) // 2))
    canvas.save(dest, "PNG", optimize=True)


def build_manifest(version_4part: str) -> bytes:
    xml = f"""<?xml version="1.0" encoding="utf-8"?>
<Package
  xmlns="http://schemas.microsoft.com/appx/manifest/foundation/windows10"
  xmlns:uap="http://schemas.microsoft.com/appx/manifest/uap/windows10"
  xmlns:rescap="http://schemas.microsoft.com/appx/manifest/foundation/windows10/restrictedcapabilities"
  IgnorableNamespaces="uap rescap">
  <Identity
    Name="{IDENTITY_NAME}"
    Publisher="{PUBLISHER}"
    Version="{version_4part}"
    ProcessorArchitecture="x64" />
  <Properties>
    <DisplayName>{DISPLAY_NAME}</DisplayName>
    <PublisherDisplayName>{PUBLISHER_DISPLAY}</PublisherDisplayName>
    <Logo>Assets\\StoreLogo.png</Logo>
  </Properties>
  <Dependencies>
    <TargetDeviceFamily Name="Windows.Desktop" MinVersion="10.0.17763.0" MaxVersionTested="10.0.22621.0" />
  </Dependencies>
  <Resources>
    <Resource Language="en-us" />
  </Resources>
  <Applications>
    <Application
      Id="OpenTermius"
      Executable="OpenTermius.exe"
      EntryPoint="Windows.FullTrustApplication">
      <uap:VisualElements
        DisplayName="{DISPLAY_NAME}"
        Description="Open-source SSH client, SFTP file manager, and encrypted team vault"
        BackgroundColor="#0a0a0f"
        Square150x150Logo="Assets\\Square150x150Logo.png"
        Square44x44Logo="Assets\\Square44x44Logo.png">
        <uap:DefaultTile
          Wide310x150Logo="Assets\\Wide310x150Logo.png"
          Square310x310Logo="Assets\\Square310x310Logo.png"
          Square71x71Logo="Assets\\Square71x71Logo.png" />
      </uap:VisualElements>
    </Application>
  </Applications>
  <Capabilities>
    <Capability Name="internetClient" />
    <rescap:Capability Name="runFullTrust" />
  </Capabilities>
</Package>
"""
    return xml.encode("utf-8")


def build_content_types() -> bytes:
    xml = """<?xml version="1.0" encoding="UTF-8"?>
<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types">
  <Default Extension="exe" ContentType="application/x-msdownload" />
  <Default Extension="png" ContentType="image/png" />
  <Default Extension="xml" ContentType="application/xml" />
  <Override PartName="/AppxManifest.xml" ContentType="application/vnd.ms-appx.manifest+xml" />
  <Override PartName="/AppxBlockMap.xml" ContentType="application/vnd.ms-appx.blockmap+xml" />
</Types>
"""
    return xml.encode("utf-8")


def build_blockmap(entries: list[tuple[str, bytes]]) -> bytes:
    lines = [
        '<?xml version="1.0" encoding="UTF-8" standalone="no"?>',
        '<BlockMap xmlns="http://schemas.microsoft.com/appx/2010/blockmap" HashMethod="http://www.w3.org/2001/04/xmlenc#sha256">',
    ]
    for arcname, data in entries:
        win_name = arcname.replace("/", "\\")
        # ZIP Local File Header size with empty extra field: 30 + len(utf8 filename)
        lfh_size = 30 + len(arcname.encode("utf-8"))
        lines.append(f'  <File Name="{win_name}" Size="{len(data)}" LfhSize="{lfh_size}">')
        if len(data) == 0:
            digest = base64.b64encode(hashlib.sha256(b"").digest()).decode("ascii")
            lines.append(f'    <Block Hash="{digest}"/>')
        else:
            for offset in range(0, len(data), BLOCK_SIZE):
                chunk = data[offset : offset + BLOCK_SIZE]
                digest = base64.b64encode(hashlib.sha256(chunk).digest()).decode("ascii")
                lines.append(f'    <Block Hash="{digest}"/>')
        lines.append("  </File>")
    lines.append("</BlockMap>")
    return ("\n".join(lines) + "\n").encode("utf-8")


def write_stored_entry(zf: zipfile.ZipFile, arcname: str, data: bytes) -> None:
    zi = zipfile.ZipInfo(filename=arcname, date_time=(2026, 11, 22, 0, 0, 0))
    zi.compress_type = zipfile.ZIP_STORED
    zi.extra = b""
    zi.external_attr = 0o644 << 16
    zf.writestr(zi, data, compress_type=zipfile.ZIP_STORED)


def main() -> int:
    if not EXE_SRC.is_file():
        print(f"ERROR: Windows binary not found at {EXE_SRC}", file=sys.stderr)
        return 1

    conf = json.loads(TAURI_CONF.read_text(encoding="utf-8"))
    ver = conf["version"]
    ver_4part = f"{ver}.0" if ver.count(".") == 2 else ver

    OUT_DIR.mkdir(parents=True, exist_ok=True)
    wide_logo = OUT_DIR / "Wide310x150Logo.png"
    ensure_wide_logo(wide_logo)

    payload_entries: list[tuple[str, bytes]] = [
        ("OpenTermius.exe", EXE_SRC.read_bytes()),
        ("Assets/StoreLogo.png", (ICONS_DIR / "StoreLogo.png").read_bytes()),
        ("Assets/Square44x44Logo.png", (ICONS_DIR / "Square44x44Logo.png").read_bytes()),
        ("Assets/Square71x71Logo.png", (ICONS_DIR / "Square71x71Logo.png").read_bytes()),
        ("Assets/Square150x150Logo.png", (ICONS_DIR / "Square150x150Logo.png").read_bytes()),
        ("Assets/Square310x310Logo.png", (ICONS_DIR / "Square310x310Logo.png").read_bytes()),
        ("Assets/Wide310x150Logo.png", wide_logo.read_bytes()),
        ("AppxManifest.xml", build_manifest(ver_4part)),
    ]

    blockmap_bytes = build_blockmap(payload_entries)
    content_types_bytes = build_content_types()

    out_msix = OUT_DIR / f"OpenTermius_{ver}_x64.msix"
    with zipfile.ZipFile(out_msix, "w") as zf:
        for arcname, data in payload_entries:
            write_stored_entry(zf, arcname, data)
        write_stored_entry(zf, "AppxBlockMap.xml", blockmap_bytes)
        write_stored_entry(zf, "[Content_Types].xml", content_types_bytes)

    wide_logo.unlink(missing_ok=True)
    size_mb = out_msix.stat().st_size / (1024 * 1024)
    print(f"Built MSIX: {out_msix} ({size_mb:.2f} MiB)")
    print(f"  Identity:  {IDENTITY_NAME} v{ver_4part} (x64)")
    print(f"  Publisher: {PUBLISHER} ({PUBLISHER_DISPLAY})")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
