; Installer Windows untuk yt-to-mp3. Lihat docs planning §25.
;
; Dibangun oleh workflow Rilis di runner Windows setelah GoReleaser:
;
;   ISCC.exe /DAppVersion=1.2.3 /DSourceExe=<path>\yt-to-mp3.exe /O<dist> packaging\windows\yt-to-mp3.iss
;
; Dipasang per pengguna tanpa hak admin, ke %LOCALAPPDATA%\Programs\yt-to-mp3.
; Data aplikasi (database, tool, log) tinggal di %LOCALAPPDATA%\yt-to-mp3 dan
; hasil konversi di folder Musik; uninstall sengaja tidak menyentuh keduanya.

#ifndef AppVersion
  #define AppVersion "0.0.0-dev"
#endif
#ifndef SourceExe
  #error SourceExe wajib diisi lewat /DSourceExe=<path ke yt-to-mp3.exe>
#endif

[Setup]
; AppId mengikat pembaruan dan uninstall ke instalasi yang sama. Jangan
; pernah diganti setelah rilis pertama.
AppId={{8D5C3E7A-4B1F-4E2A-9C6D-2F7B1A3E5D90}
AppName=yt-to-mp3
AppVersion={#AppVersion}
AppVerName=yt-to-mp3 {#AppVersion}
AppPublisher=Irfana Dwi Fangga
DefaultDirName={autopf}\yt-to-mp3
DefaultGroupName=yt-to-mp3
DisableProgramGroupPage=yes
DisableDirPage=auto
PrivilegesRequired=lowest
ArchitecturesAllowed=x64compatible
ArchitecturesInstallIn64BitMode=x64compatible
LicenseFile=..\..\LICENSE
SetupIconFile=yt-to-mp3.ico
UninstallDisplayIcon={app}\yt-to-mp3.exe
UninstallDisplayName=yt-to-mp3
OutputBaseFilename=yt-to-mp3_{#AppVersion}_windows_amd64_setup
Compression=lzma2
SolidCompression=yes
WizardStyle=modern
; Aplikasi berjalan tanpa jendela, jadi pengguna mungkin tidak sadar ia
; masih hidup. Restart Manager menutupnya sebelum berkas diganti.
CloseApplications=yes
RestartApplications=no

[Languages]
Name: "english"; MessagesFile: "compiler:Default.isl"

[Tasks]
Name: "desktopicon"; Description: "{cm:CreateDesktopIcon}"; GroupDescription: "{cm:AdditionalIcons}"; Flags: unchecked

[Files]
Source: "{#SourceExe}"; DestDir: "{app}"; DestName: "yt-to-mp3.exe"; Flags: ignoreversion
Source: "..\..\LICENSE"; DestDir: "{app}"; DestName: "LICENSE.txt"; Flags: ignoreversion

[Icons]
Name: "{autoprograms}\yt-to-mp3"; Filename: "{app}\yt-to-mp3.exe"
Name: "{autodesktop}\yt-to-mp3"; Filename: "{app}\yt-to-mp3.exe"; Tasks: desktopicon

[Run]
Filename: "{app}\yt-to-mp3.exe"; Description: "{cm:LaunchProgram,yt-to-mp3}"; Flags: nowait postinstall skipifsilent
