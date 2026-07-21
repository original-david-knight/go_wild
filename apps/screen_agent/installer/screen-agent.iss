; Inno Setup script for screen-agent (Windows).
; Build with installer\build.ps1, or directly:
;   ISCC.exe /DMyAppVersion=0.1.0 screen-agent.iss
; Unattended install:
;   screen-agent-setup-<version>.exe /VERYSILENT /ApiKey=<GEMINI_API_KEY>

#ifndef MyAppVersion
#define MyAppVersion "0.1.0"
#endif

[Setup]
AppId={{5F0C0D0E-9B7A-4A57-B1D4-2A6B77E3C9D1}
AppName=screen-agent
AppVersion={#MyAppVersion}
AppPublisher=go_wild
DefaultDirName={autopf}\screen-agent
DisableProgramGroupPage=yes
PrivilegesRequired=lowest
OutputDir=output
OutputBaseFilename=screen-agent-setup-{#MyAppVersion}
Compression=lzma2
SolidCompression=yes
ArchitecturesInstallIn64BitMode=x64compatible
ChangesEnvironment=yes
UninstallDisplayIcon={app}\screen-agent.exe

[Files]
Source: "screen-agent.exe"; DestDir: "{app}"; Flags: ignoreversion
Source: "..\prompt.md"; DestDir: "{app}"; Flags: ignoreversion
Source: "daemon-hidden.vbs"; DestDir: "{app}"; Flags: ignoreversion

[Tasks]
Name: "autostart"; Description: "Start the screen-agent daemon automatically at login"
Name: "addtopath"; Description: "Add screen-agent to your PATH (for `screen-agent assist` etc.)"

[Icons]
Name: "{userstartup}\screen-agent daemon"; Filename: "{sys}\wscript.exe"; Parameters: """{app}\daemon-hidden.vbs"""; Tasks: autostart
Name: "{autoprograms}\screen-agent daemon"; Filename: "{sys}\wscript.exe"; Parameters: """{app}\daemon-hidden.vbs"""

[Run]
Filename: "{sys}\wscript.exe"; Parameters: """{app}\daemon-hidden.vbs"""; Description: "Start the screen-agent daemon now"; Flags: postinstall nowait skipifsilent

[UninstallRun]
Filename: "{cmd}"; Parameters: "/C taskkill /F /IM screen-agent.exe"; Flags: runhidden; RunOnceId: "KillDaemon"

[Code]
var
  ApiKeyPage: TInputQueryWizardPage;

function ConfigDir(): String;
begin
  Result := ExpandConstant('{%USERPROFILE}') + '\.config\screen-agent';
end;

function EnvFilePath(): String;
begin
  Result := ConfigDir() + '\.env';
end;

function ConfigFilePath(): String;
begin
  Result := ConfigDir() + '\config.toml';
end;

function ApiKeyValue(): String;
begin
  Result := Trim(ExpandConstant('{param:ApiKey|}'));
  if (Result = '') and (ApiKeyPage <> nil) then
    Result := Trim(ApiKeyPage.Values[0]);
end;

procedure InitializeWizard();
begin
  ApiKeyPage := CreateInputQueryPage(wpSelectTasks,
    'Gemini API key',
    'screen-agent uses the Gemini API for screen analysis and speech.',
    'Enter your GEMINI_API_KEY. It is stored in ' + EnvFilePath() + '.'
    + #13#10 + 'Get a key at https://aistudio.google.com/apikey.');
  ApiKeyPage.Add('API key:', True);
  ApiKeyPage.Values[0] := Trim(ExpandConstant('{param:ApiKey|}'));
end;

function NextButtonClick(CurPageID: Integer): Boolean;
begin
  Result := True;
  if (ApiKeyPage <> nil) and (CurPageID = ApiKeyPage.ID) then
  begin
    if (Trim(ApiKeyPage.Values[0]) = '') and not FileExists(EnvFilePath()) then
    begin
      MsgBox('A Gemini API key is required. Enter one, or create '
        + EnvFilePath() + ' yourself before running screen-agent.', mbError, MB_OK);
      Result := False;
    end;
  end;
end;

function PrepareToInstall(var NeedsRestart: Boolean): String;
var
  ResultCode: Integer;
begin
  { Replace-in-place fails while the daemon holds the exe open. }
  Exec(ExpandConstant('{cmd}'), '/C taskkill /F /IM screen-agent.exe',
    '', SW_HIDE, ewWaitUntilTerminated, ResultCode);
  Result := '';
end;

function TomlPath(const Path: String): String;
begin
  { TOML basic strings treat backslashes as escapes; Go accepts forward slashes. }
  Result := Path;
  StringChangeEx(Result, '\', '/', True);
end;

procedure WriteDefaultConfig();
var
  Content: String;
begin
  if FileExists(ConfigFilePath()) then
    Exit;
  Content :=
    '# screen-agent configuration' + #13#10 +
    'hotkey = "ctrl+shift+a"' + #13#10 +
    'agent_prompt_path = "' + TomlPath(ExpandConstant('{app}')) + '/prompt.md"' + #13#10;
  if not SaveStringToFile(ConfigFilePath(), Content, False) then
    MsgBox('Could not write ' + ConfigFilePath(), mbError, MB_OK);
end;

procedure WriteApiKey();
var
  Key: String;
begin
  Key := ApiKeyValue();
  if Key = '' then
    Exit;
  if not SaveStringToFile(EnvFilePath(), 'GEMINI_API_KEY=' + Key + #13#10, False) then
    MsgBox('Could not write ' + EnvFilePath(), mbError, MB_OK);
end;

procedure AddAppToUserPath();
var
  Path: String;
  AppDir: String;
begin
  AppDir := ExpandConstant('{app}');
  if not RegQueryStringValue(HKEY_CURRENT_USER, 'Environment', 'Path', Path) then
    Path := '';
  if Pos(';' + Lowercase(AppDir) + ';', ';' + Lowercase(Path) + ';') > 0 then
    Exit;
  if (Path <> '') and (Copy(Path, Length(Path), 1) <> ';') then
    Path := Path + ';';
  RegWriteExpandStringValue(HKEY_CURRENT_USER, 'Environment', 'Path', Path + AppDir);
end;

procedure RemoveAppFromUserPath();
var
  Path: String;
  AppDir: String;
  P: Integer;
begin
  AppDir := ExpandConstant('{app}');
  if not RegQueryStringValue(HKEY_CURRENT_USER, 'Environment', 'Path', Path) then
    Exit;
  P := Pos(';' + Lowercase(AppDir), ';' + Lowercase(Path));
  if P = 0 then
    Exit;
  Delete(Path, P, Length(AppDir) + 1);
  if Copy(Path, 1, 1) = ';' then
    Delete(Path, 1, 1);
  RegWriteExpandStringValue(HKEY_CURRENT_USER, 'Environment', 'Path', Path);
end;

procedure CurStepChanged(CurStep: TSetupStep);
begin
  if CurStep = ssPostInstall then
  begin
    if not ForceDirectories(ConfigDir()) then
    begin
      MsgBox('Could not create ' + ConfigDir(), mbError, MB_OK);
      Exit;
    end;
    WriteApiKey();
    WriteDefaultConfig();
    if WizardIsTaskSelected('addtopath') then
      AddAppToUserPath();
  end;
end;

procedure CurUninstallStepChanged(CurUninstallStep: TUninstallStep);
begin
  { Config and the API key in %USERPROFILE%\.config\screen-agent are kept. }
  if CurUninstallStep = usPostUninstall then
    RemoveAppFromUserPath();
end;
