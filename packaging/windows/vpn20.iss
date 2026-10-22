#ifndef Version
  #error Version must be supplied by build.ps1
#endif
#ifndef SourceDir
  #error SourceDir must be supplied by build.ps1
#endif
#ifndef OutputDir
  #error OutputDir must be supplied by build.ps1
#endif
#ifndef Architecture
  #define Architecture "x64"
#endif

[Setup]
AppId={{F46A58C2-8B71-4EA8-BFF5-D2C0C0BB5020}
AppName=VPN20
AppVersion={#Version}
AppPublisher=Công ty TNHH TN20
AppPublisherURL=https://20.com.vn
AppSupportURL=https://github.com/lehuunghi/vpn/issues
DefaultDirName={autopf}\VPN20
DefaultGroupName=VPN20
DisableProgramGroupPage=yes
LicenseFile={#SourceDir}\LICENSE
InfoAfterFile={#SourceDir}\README-Windows.md
OutputDir={#OutputDir}
OutputBaseFilename=VPN20-{#Version}-windows-{#Architecture}-setup
SetupIconFile=..\..\web\public\favicon.ico
UninstallDisplayIcon={app}\vpn20.ico
Compression=lzma2
SolidCompression=yes
WizardStyle=modern
PrivilegesRequired=admin
#if Architecture == "x64"
ArchitecturesAllowed=x64compatible
ArchitecturesInstallIn64BitMode=x64compatible
#else
ArchitecturesAllowed=x86compatible
#endif
MinVersion=10.0.14393
CloseApplications=no
RestartApplications=no
SetupLogging=yes
#ifdef SignedBuild
SignTool=vpn20sign
SignedUninstaller=yes
SignToolRetryCount=3
#endif

[Languages]
Name: "english"; MessagesFile: "compiler:Default.isl"

[Tasks]
Name: "desktopicon"; Description: "Create a desktop shortcut to VPN20"; Flags: unchecked

[Dirs]
Name: "{commonappdata}\VPN20"

[Files]
Source: "{#SourceDir}\*"; DestDir: "{app}"; Flags: ignoreversion recursesubdirs createallsubdirs; Excludes: "config.example.json"
Source: "{#SourceDir}\config.example.json"; DestDir: "{commonappdata}\VPN20"; DestName: "config.json"; Flags: onlyifdoesntexist uninsneveruninstall

[Icons]
Name: "{group}\VPN20"; Filename: "http://127.0.0.1:51821"; IconFilename: "{app}\vpn20.ico"
Name: "{group}\Uninstall VPN20"; Filename: "{uninstallexe}"
Name: "{autodesktop}\VPN20"; Filename: "http://127.0.0.1:51821"; IconFilename: "{app}\vpn20.ico"; Tasks: desktopicon

[Run]
Filename: "http://127.0.0.1:51821"; Description: "Open VPN20 administration"; Flags: shellexec postinstall nowait skipifsilent runasoriginaluser; Check: ServiceReady

[Code]
var
  StartupFailed: Boolean;

function ServiceReady(): Boolean;
begin
  Result := not StartupFailed;
end;

function GetCustomSetupExitCode(): Integer;
begin
  if StartupFailed then Result := 1 else Result := 0;
end;

function RunServiceCommand(Command: String): Boolean;
var
  ExitCode: Integer;
begin
  Result := Exec(ExpandConstant('{app}\vpn20.exe'), Command, '', SW_HIDE,
    ewWaitUntilTerminated, ExitCode) and (ExitCode = 0);
end;

function PrepareToInstall(var NeedsRestart: Boolean): String;
begin
  Result := '';
  if FileExists(ExpandConstant('{app}\vpn20.exe')) then
    if not RunServiceCommand('stop-service') then
      Result := 'Cannot stop VPN20. Close the existing service before upgrading.';
end;

procedure CurStepChanged(CurStep: TSetupStep);
var
  Lines: TArrayOfString;
  Detail: String;
  I: Integer;
begin
  if CurStep = ssPostInstall then
    if not RunServiceCommand('install-service') then
    begin
      StartupFailed := True;
      Detail := 'VPN20 could not start.';
      if LoadStringsFromFile(ExpandConstant('{commonappdata}\VPN20\install-error.txt'), Lines) then
        for I := 0 to GetArrayLength(Lines) - 1 do
          Detail := Detail + #13#10 + Lines[I];
      RaiseException(Detail + #13#10#13#10 + 'Full log: ' + ExpandConstant('{commonappdata}\VPN20\vpn20.log'));
    end;
end;

function InitializeUninstall(): Boolean;
begin
  Result := RunServiceCommand('uninstall-service');
  if not Result then
    MsgBox('Cannot stop or remove the VPN20 service. Run the uninstaller as Administrator.', mbError, MB_OK);
end;
