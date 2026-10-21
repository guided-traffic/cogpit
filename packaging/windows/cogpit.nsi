; Cogpit's Windows setup (D-111; docs/developer/releasing.md#windows): one installer for the app,
; its command-line tool and its updater, built by hack/windows/setup.sh with NSIS 3 (makensis) from
; the files `make dist-windows` staged. Every choice below is the lead's (2026-10-21), open to
; revision, but the per-user default, /allusers, /S and /D=, which the owner chose as Lens has them.
;
;   Cogpit-<version>-windows-<arch>-setup.exe [/S] [/allusers | /currentuser] [/relaunch] [/D=<folder>]
;
; - Per user by default, without an administrator: %LOCALAPPDATA%\Programs\Cogpit, the user's Start
;   menu, the uninstall entry in HKCU. So WinSparkle's updates install without a UAC prompt.
; - /allusers installs for every user of the machine: Program Files, the common Start menu, HKLM.
;   Without an administrator's token the setup starts itself again through UAC ("runas") with the
;   same options; a declined prompt ends it with ERROR_CANCELLED (1223).
; - Without either option an existing installation is updated where it is and as it was installed
;   (its uninstall entry's InstallLocation; a per-user one wins when there are both).
; - /S installs silently (NSIS's own option); /D= chooses the folder and comes last, as NSIS wants.
; - /relaunch starts Cogpit once installed: WinSparkle runs the setup as "/S /relaunch"
;   (the appcast's sparkle:installerArguments) and asks the running app to quit right after.
;
; A running cogpit.exe cannot be replaced: the setup waits for it (silently up to a minute, else it
; asks), so an update from WinSparkle installs once the app has quit. A running cogpit-cli.exe (an
; agent's MCP bridge, which may run for hours) is moved aside instead (<random>.cogpit-cli.old) and
; removed by the next setup or uninstall that finds it free. The user's own files (%APPDATA%\Cogpit, %LOCALAPPDATA%\Cogpit)
; are never touched: uninstalling keeps them, as `apt remove` does on Linux.

Unicode true
ManifestDPIAware true
ManifestSupportedOS {8e0f7a12-bfb3-4fe8-b9a5-48fd50a15a9a} ; Windows 10 and 11
SetCompressor /SOLID lzma
RequestExecutionLevel user

!ifndef VERSION | STAGE | OUTFILE | ARCH
	!error "makensis needs -DVERSION=x.y.z -DARCH=x64|arm64 -DSTAGE=<staged files> -DOUTFILE=<setup.exe>"
!endif

!include "MUI2.nsh"
!include "LogicLib.nsh"
!include "FileFunc.nsh"
!include "x64.nsh"
!include "WinVer.nsh"

!define APP "Cogpit"
!define APP_ID "io.guided-traffic.cogpit"
!define PUBLISHER "guided-traffic"
!define HOME "https://github.com/guided-traffic/cogpit"
!define UNINSTALL_KEY "Software\Microsoft\Windows\CurrentVersion\Uninstall\${APP_ID}"
; Where the app tells WinSparkle to keep its state (internal/update/winsparkle_windows.go).
!define WINSPARKLE_KEY "Software\guided-traffic\Cogpit\WinSparkle"

Name "${APP}"
OutFile "${OUTFILE}"
BrandingText "${APP} ${VERSION}"
ShowInstDetails nevershow
ShowUninstDetails nevershow

VIProductVersion "${VERSION}.0"
VIFileVersion "${VERSION}.0"
VIAddVersionKey "ProductName" "${APP}"
VIAddVersionKey "CompanyName" "${PUBLISHER}"
VIAddVersionKey "FileDescription" "${APP} Setup"
VIAddVersionKey "FileVersion" "${VERSION}"
VIAddVersionKey "ProductVersion" "${VERSION}"
VIAddVersionKey "LegalCopyright" "Copyright 2026 The Cogpit authors, Apache License 2.0"
VIAddVersionKey "Comments" "${HOME}"

Var AllUsers ; 1: for every user (Program Files, HKLM), else for this user
Var Relaunch ; 1: start Cogpit once installed (/relaunch)

!define MUI_ICON "${STAGE}\cogpit.ico"
!define MUI_UNICON "${STAGE}\cogpit.ico"
!define MUI_ABORTWARNING
!define MUI_FINISHPAGE_RUN
!define MUI_FINISHPAGE_RUN_TEXT "Start ${APP}"
!define MUI_FINISHPAGE_RUN_FUNCTION StartCogpit

!insertmacro MUI_PAGE_WELCOME
!insertmacro MUI_PAGE_DIRECTORY
!insertmacro MUI_PAGE_INSTFILES
!insertmacro MUI_PAGE_FINISH
!insertmacro MUI_UNPAGE_CONFIRM
!insertmacro MUI_UNPAGE_INSTFILES
!insertmacro MUI_LANGUAGE "English"

; IsAdmin pushes whether this process holds an administrator's token: an administrator's setup
; that UAC did not elevate does not.
!macro IsAdmin
	UserInfo::GetAccountType
!macroend

; WaitForCogpit waits until no cogpit.exe runs from $INSTDIR: a running program's file cannot be
; opened for writing. Silently it waits up to a minute (WinSparkle asks the app to quit right after
; it started the setup), else it asks to close Cogpit; given up, the setup fails.
!macro WaitForCogpit un
	Function ${un}WaitForCogpit
		${IfNot} ${FileExists} "$INSTDIR\cogpit.exe"
			Return
		${EndIf}
		StrCpy $R9 0
		${Do}
			ClearErrors
			FileOpen $R8 "$INSTDIR\cogpit.exe" a
			${IfNot} ${Errors}
				FileClose $R8
				Return
			${EndIf}
			${If} ${Silent}
				${If} $R9 >= 60
					SetErrorLevel 5 ; ERROR_ACCESS_DENIED: the file stays in use
					Abort "Cogpit is still running from $INSTDIR."
				${EndIf}
				IntOp $R9 $R9 + 1
				Sleep 1000
			${Else}
				MessageBox MB_RETRYCANCEL|MB_ICONEXCLAMATION "Cogpit is running. Close Cogpit, then click Retry." IDRETRY retry
				Abort "Cogpit is still running from $INSTDIR."
				retry:
			${EndIf}
		${Loop}
	FunctionEnd
!macroend
!insertmacro WaitForCogpit ""
!insertmacro WaitForCogpit "un."

; RemoveOldCLIs removes the cogpit-cli.exe copies an earlier setup moved aside while they ran.
!macro RemoveOldCLIs
	FindFirst $R7 $R6 "$INSTDIR\*.cogpit-cli.old"
	${DoWhile} $R6 != ""
		Delete "$INSTDIR\$R6"
		FindNext $R7 $R6
	${Loop}
	FindClose $R7
!macroend

Function .onInit
	${IfNot} ${RunningX64}
		MessageBox MB_OK|MB_ICONSTOP "Cogpit needs a 64-bit Windows." /SD IDOK
		SetErrorLevel 1150 ; ERROR_OLD_WIN_VERSION
		Quit
	${EndIf}
	; Windows 10 1809 (build 17763) brought ConPTY, the local terminal's console (the feed's least
	; system too, hack/windows/appcast.sh).
	${IfNot} ${AtLeastWin10}
	${OrIfNot} ${AtLeastBuild} 17763
		MessageBox MB_OK|MB_ICONSTOP "Cogpit needs Windows 10 version 1809 or later." /SD IDOK
		SetErrorLevel 1150
		Quit
	${EndIf}
	; NSIS runs as a 32-bit program: the machine's uninstall entry goes into HKLM's 64-bit view,
	; where the 64-bit app looks for it (internal/update).
	SetRegView 64
	${GetParameters} $R0
	StrCpy $Relaunch 0
	ClearErrors
	${GetOptions} $R0 "/relaunch" $R1
	${IfNot} ${Errors}
		StrCpy $Relaunch 1
	${EndIf}

	; The mode: as asked, else as the existing installation is, else for this user.
	ReadRegStr $R2 HKCU "${UNINSTALL_KEY}" "InstallLocation"
	ReadRegStr $R3 HKLM "${UNINSTALL_KEY}" "InstallLocation"
	StrCpy $AllUsers 0
	${If} $R2 == ""
	${AndIf} $R3 != ""
		StrCpy $AllUsers 1
	${EndIf}
	ClearErrors
	${GetOptions} $R0 "/allusers" $R1
	${IfNot} ${Errors}
		StrCpy $AllUsers 1
	${EndIf}
	ClearErrors
	${GetOptions} $R0 "/currentuser" $R1
	${IfNot} ${Errors}
		StrCpy $AllUsers 0
	${EndIf}

	${If} $AllUsers == 1
		!insertmacro IsAdmin
		Pop $R4
		${If} $R4 != "Admin"
			; Again, elevated, with the same options; the folder too when /D= chose it.
			StrCpy $R5 "$R0 /allusers"
			${If} $INSTDIR != ""
				StrCpy $R5 "$R5 /D=$INSTDIR"
			${EndIf}
			ClearErrors
			ExecShellWait "runas" "$EXEPATH" "$R5"
			${If} ${Errors}
				MessageBox MB_OK|MB_ICONSTOP "Installing Cogpit for all users needs an administrator." /SD IDOK
				SetErrorLevel 1223 ; ERROR_CANCELLED
			${EndIf}
			Quit
		${EndIf}
		SetShellVarContext all
		StrCpy $R6 $R3
		StrCpy $R7 "$PROGRAMFILES64\${APP}"
	${Else}
		SetShellVarContext current
		StrCpy $R6 $R2
		StrCpy $R7 "$LOCALAPPDATA\Programs\${APP}"
	${EndIf}
	${If} $INSTDIR == "" ; no /D=
		${If} $R6 != ""
			StrCpy $INSTDIR $R6
		${Else}
			StrCpy $INSTDIR $R7
		${EndIf}
	${EndIf}
FunctionEnd

; StartCogpit starts the installed app as the desktop's user: through Explorer when this setup runs
; elevated (/allusers), so that Cogpit does not inherit the administrator's token.
Function StartCogpit
	${If} $AllUsers == 1
		Exec '"$WINDIR\explorer.exe" "$INSTDIR\cogpit.exe"'
	${Else}
		Exec '"$INSTDIR\cogpit.exe"'
	${EndIf}
FunctionEnd

Section "Cogpit"
	SetOutPath "$INSTDIR"
	Call WaitForCogpit
	!insertmacro RemoveOldCLIs
	; A cogpit-cli.exe that runs cannot be replaced but can be renamed: move it aside.
	${If} ${FileExists} "$INSTDIR\cogpit-cli.exe"
		ClearErrors
		Delete "$INSTDIR\cogpit-cli.exe"
		${If} ${Errors}
			GetTempFileName $R0 "$INSTDIR"
			Delete $R0
			Rename "$INSTDIR\cogpit-cli.exe" "$R0.cogpit-cli.old"
		${EndIf}
	${EndIf}

	File "${STAGE}\cogpit.exe"
	File "${STAGE}\cogpit-cli.exe"
	File "${STAGE}\WinSparkle.dll"
	File "${STAGE}\LICENSE.txt"
	File "${STAGE}\THIRD-PARTY-NOTICES.txt"
	WriteUninstaller "$INSTDIR\uninstall.exe"

	CreateShortcut "$SMPROGRAMS\${APP}.lnk" "$INSTDIR\cogpit.exe" "" "$INSTDIR\cogpit.exe" 0 SW_SHOWNORMAL "" "Kubernetes IDE"

	; The uninstall entry, in HKCU or HKLM as the mode is (SHCTX follows SetShellVarContext).
	${If} $AllUsers == 1
		WriteRegStr SHCTX "${UNINSTALL_KEY}" "UninstallString" '"$INSTDIR\uninstall.exe" /allusers'
		WriteRegStr SHCTX "${UNINSTALL_KEY}" "QuietUninstallString" '"$INSTDIR\uninstall.exe" /allusers /S'
	${Else}
		WriteRegStr SHCTX "${UNINSTALL_KEY}" "UninstallString" '"$INSTDIR\uninstall.exe"'
		WriteRegStr SHCTX "${UNINSTALL_KEY}" "QuietUninstallString" '"$INSTDIR\uninstall.exe" /S'
	${EndIf}
	WriteRegStr SHCTX "${UNINSTALL_KEY}" "DisplayName" "${APP}"
	WriteRegStr SHCTX "${UNINSTALL_KEY}" "DisplayVersion" "${VERSION}"
	WriteRegStr SHCTX "${UNINSTALL_KEY}" "Publisher" "${PUBLISHER}"
	WriteRegStr SHCTX "${UNINSTALL_KEY}" "DisplayIcon" "$INSTDIR\cogpit.exe,0"
	WriteRegStr SHCTX "${UNINSTALL_KEY}" "InstallLocation" "$INSTDIR"
	WriteRegStr SHCTX "${UNINSTALL_KEY}" "URLInfoAbout" "${HOME}"
	WriteRegStr SHCTX "${UNINSTALL_KEY}" "HelpLink" "${HOME}/issues"
	WriteRegStr SHCTX "${UNINSTALL_KEY}" "URLUpdateInfo" "${HOME}/releases"
	WriteRegDWORD SHCTX "${UNINSTALL_KEY}" "NoModify" 1
	WriteRegDWORD SHCTX "${UNINSTALL_KEY}" "NoRepair" 1
	${GetSize} "$INSTDIR" "/S=0K" $R0 $R1 $R2
	WriteRegDWORD SHCTX "${UNINSTALL_KEY}" "EstimatedSize" "$R0"

	${If} $Relaunch == 1
		Call StartCogpit
	${EndIf}
SectionEnd

Function un.onInit
	SetRegView 64
	${GetParameters} $R0
	StrCpy $AllUsers 0
	ClearErrors
	${GetOptions} $R0 "/allusers" $R1
	${IfNot} ${Errors}
		StrCpy $AllUsers 1
	${EndIf}
	${If} $AllUsers == 1
		!insertmacro IsAdmin
		Pop $R4
		${If} $R4 != "Admin"
			ClearErrors
			ExecShellWait "runas" "$EXEPATH" "$R0"
			${If} ${Errors}
				MessageBox MB_OK|MB_ICONSTOP "Removing Cogpit for all users needs an administrator." /SD IDOK
				SetErrorLevel 1223 ; ERROR_CANCELLED
			${EndIf}
			Quit
		${EndIf}
		SetShellVarContext all
	${Else}
		SetShellVarContext current
	${EndIf}
FunctionEnd

Section "Uninstall"
	Call un.WaitForCogpit
	Delete "$INSTDIR\cogpit.exe"
	Delete /REBOOTOK "$INSTDIR\cogpit-cli.exe"
	Delete "$INSTDIR\WinSparkle.dll"
	Delete "$INSTDIR\LICENSE.txt"
	Delete "$INSTDIR\THIRD-PARTY-NOTICES.txt"
	!insertmacro RemoveOldCLIs
	Delete "$INSTDIR\uninstall.exe"
	RMDir /REBOOTOK "$INSTDIR"
	Delete "$SMPROGRAMS\${APP}.lnk"
	DeleteRegKey SHCTX "${UNINSTALL_KEY}"
	; WinSparkle's state (the last check, a skipped version) goes with the app; Cogpit's own
	; settings stay.
	DeleteRegKey HKCU "${WINSPARKLE_KEY}"
	DeleteRegKey /ifempty HKCU "Software\guided-traffic\Cogpit"
	DeleteRegKey /ifempty HKCU "Software\guided-traffic"
SectionEnd
