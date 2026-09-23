; Per-user installer. Puts unreal-agent-runner on PATH.
; Version is passed by CI: ISCC /DVersion=<sha7>

#ifndef Version
  #define Version "dev"
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
OutputBaseFilename=unreal-agent-plus_{#Version}_windows_amd64
Compression=lzma2
SolidCompression=yes
ArchitecturesAllowed=x64
ArchitecturesInstallIn64BitMode=x64
PrivilegesRequired=lowest
ChangesEnvironment=yes
WizardStyle=modern
UninstallDisplayName=unreal-agent++

[Files]
Source: "unreal-agent-runner.exe"; DestDir: "{app}"; Flags: ignoreversion

[Icons]
Name: "{autoprograms}\unreal-agent++"; Filename: "{app}\unreal-agent-runner.exe"

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
