; Installs the harness for the current user and puts it on PATH.
; CI: ISCC /DVersion=<sha7> /DArch=amd64|arm64

#ifndef Version
  #define Version "dev"
#endif
#ifndef Arch
  #define Arch "amd64"
#endif

[Setup]
AppId={{B7E1C4A2-9D30-4F6A-8C15-2A6E0D4F8B31}
AppName=unreal-agent++
AppVersion={#Version}
AppPublisher=stacc
DefaultDirName={localappdata}\Programs\unreal-agent
DisableProgramGroupPage=yes
DisableDirPage=auto
OutputDir=..\..\dist
OutputBaseFilename=unreal-agent-plus_{#Version}_windows_{#Arch}
Compression=lzma2
SolidCompression=yes
#if Arch == "arm64"
ArchitecturesAllowed=arm64
ArchitecturesInstallIn64BitMode=arm64
#else
ArchitecturesAllowed=x64
ArchitecturesInstallIn64BitMode=x64
#endif
PrivilegesRequired=lowest
ChangesEnvironment=yes
WizardStyle=modern
UninstallDisplayName=unreal-agent++

[Files]
Source: "unreal-agent-runner.exe"; DestDir: "{app}"; DestName: "unreal-agent++.exe"; Flags: ignoreversion

[Icons]
Name: "{autoprograms}\unreal-agent++"; Filename: "{app}\unreal-agent++.exe"

[Run]
Filename: "{app}\unreal-agent++.exe"; Description: "Open ChatGPT"; Flags: nowait postinstall skipifsilent

[Registry]
Root: HKCU; Subkey: "Environment"; ValueType: expandsz; ValueName: "Path"; ValueData: "{olddata};{app}"; Check: NeedsAddPath(ExpandConstant('{app}'))

[Code]
function NeedsAddPath(Param: string): boolean;
var
  OrigPath: string;
begin
  if not RegQueryStringValue(HKCU, 'Environment', 'Path', OrigPath) then
  begin
    Result := True;
    exit;
  end;
  Result := Pos(';' + Uppercase(Param) + ';', ';' + Uppercase(OrigPath) + ';') = 0;
end;
