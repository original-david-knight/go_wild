' Launches the screen-agent daemon without a console window.
Option Explicit
Dim fso, shell, dir
Set fso = CreateObject("Scripting.FileSystemObject")
Set shell = CreateObject("WScript.Shell")
dir = fso.GetParentFolderName(WScript.ScriptFullName)
shell.Run """" & dir & "\screen-agent.exe"" daemon", 0, False
