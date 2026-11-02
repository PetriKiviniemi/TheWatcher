; Per-user installer: no admin rights needed. Settings in %APPDATA%\Tonttivahti are kept on uninstall.
Unicode true
!include "MUI2.nsh"

!define APP "Tonttivahti"
!define EXE "tonttivahti.exe"
!define UNINSTALL_KEY "Software\Microsoft\Windows\CurrentVersion\Uninstall\${APP}"
!ifndef VERSION
  !error "Build with build.sh, or pass -DVERSION=x.y.z"
!endif
!ifdef SIGN_CMD
  !uninstfinalize '${SIGN_CMD} "%1"' = 0 ; sign the uninstaller too; fail the build if signing fails
!endif

Name "${APP}"
OutFile "build\TonttivahtiSetup.exe"
InstallDir "$LOCALAPPDATA\Programs\${APP}"
RequestExecutionLevel user

!define MUI_FINISHPAGE_RUN "$INSTDIR\${EXE}"
!define MUI_FINISHPAGE_RUN_TEXT "Open ${APP} now"
!insertmacro MUI_PAGE_INSTFILES
!insertmacro MUI_PAGE_FINISH
!insertmacro MUI_UNPAGE_CONFIRM
!insertmacro MUI_UNPAGE_INSTFILES
!insertmacro MUI_LANGUAGE "English"

Section "Install"
  nsExec::Exec 'taskkill /IM ${EXE} /F' ; an upgrade cannot replace a running exe
  Sleep 1000 ; let Windows release the file lock
  SetOutPath "$INSTDIR"
  File "build\${EXE}"
  WriteUninstaller "$INSTDIR\uninstall.exe"

  CreateShortCut "$SMPROGRAMS\${APP}.lnk" "$INSTDIR\${EXE}"
  ; Start watching at login without opening the browser.
  CreateShortCut "$SMSTARTUP\${APP}.lnk" "$INSTDIR\${EXE}" "-background"

  WriteRegStr HKCU "${UNINSTALL_KEY}" "DisplayName" "${APP}"
  WriteRegStr HKCU "${UNINSTALL_KEY}" "DisplayVersion" "${VERSION}"
  WriteRegStr HKCU "${UNINSTALL_KEY}" "DisplayIcon" "$INSTDIR\${EXE}"
  WriteRegStr HKCU "${UNINSTALL_KEY}" "UninstallString" '"$INSTDIR\uninstall.exe"'
  WriteRegDWORD HKCU "${UNINSTALL_KEY}" "NoModify" 1
  WriteRegDWORD HKCU "${UNINSTALL_KEY}" "NoRepair" 1

  ; Keep watching right away; "Open now" on the finish page then just opens the browser.
  Exec '"$INSTDIR\${EXE}" -background'
SectionEnd

Section "Uninstall"
  nsExec::Exec 'taskkill /IM ${EXE} /F'
  Delete "$SMPROGRAMS\${APP}.lnk"
  Delete "$SMSTARTUP\${APP}.lnk"
  Delete "$INSTDIR\${EXE}"
  Delete "$INSTDIR\uninstall.exe"
  RMDir "$INSTDIR"
  DeleteRegKey HKCU "${UNINSTALL_KEY}"
SectionEnd
