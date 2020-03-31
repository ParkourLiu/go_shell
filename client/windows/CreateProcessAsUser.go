package main

import (
	"fmt"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	modwtsapi32                      *windows.LazyDLL  = windows.NewLazySystemDLL("wtsapi32.dll")
	modkernel32                      *windows.LazyDLL  = windows.NewLazySystemDLL("kernel32.dll")
	modadvapi32                      *windows.LazyDLL  = windows.NewLazySystemDLL("advapi32.dll")
	moduserenv                       *windows.LazyDLL  = windows.NewLazySystemDLL("userenv.dll")
	procWTSEnumerateSessionsW        *windows.LazyProc = modwtsapi32.NewProc("WTSEnumerateSessionsW")
	procWTSGetActiveConsoleSessionId *windows.LazyProc = modkernel32.NewProc("WTSGetActiveConsoleSessionId")
	procWTSQueryUserToken            *windows.LazyProc = modwtsapi32.NewProc("WTSQueryUserToken")
	procDuplicateTokenEx             *windows.LazyProc = modadvapi32.NewProc("DuplicateTokenEx")
	procCreateEnvironmentBlock       *windows.LazyProc = moduserenv.NewProc("CreateEnvironmentBlock")
	procCreateProcessAsUser          *windows.LazyProc = modadvapi32.NewProc("CreateProcessAsUserW")
	procGetTokenInformation          *windows.LazyProc = modadvapi32.NewProc("GetTokenInformation")
)

type WTS_CONNECTSTATE_CLASS int
type SECURITY_IMPERSONATION_LEVEL int
type TOKEN_TYPE int
type SW int
type WTS_SESSION_INFO struct {
	SessionID      windows.Handle
	WinStationName *uint16
	State          WTS_CONNECTSTATE_CLASS
}
type TOKEN_LINKED_TOKEN struct {
	LinkedToken windows.Token
}

const (
	WTS_CURRENT_SERVER_HANDLE uintptr = 0
)
const (
	WTSActive WTS_CONNECTSTATE_CLASS = iota
	WTSConnected
	WTSConnectQuery
	WTSShadow
	WTSDisconnected
	WTSIdle
	WTSListen
	WTSReset
	WTSDown
	WTSInit
)
const (
	SecurityAnonymous SECURITY_IMPERSONATION_LEVEL = iota
	SecurityIdentification
	SecurityImpersonation
	SecurityDelegation
)
const (
	TokenPrimary TOKEN_TYPE = iota + 1
	TokenImpersonazion
)
const (
	SW_HIDE            SW = 0
	SW_SHOWNORMAL         = 1
	SW_NORMAL             = 1
	SW_SHOWMINIMIZED      = 2
	SW_SHOWMAXIMIZED      = 3
	SW_MAXIMIZE           = 3
	SW_SHOWNOACTIVATE     = 4
	SW_SHOW               = 5
	SW_MINIMIZE           = 6
	SW_SHOWMINNOACTIVE    = 7
	SW_SHOWNA             = 8
	SW_RESTORE            = 9
	SW_SHOWDEFAULT        = 10
	SW_MAX                = 1
)
const (
	CREATE_UNICODE_ENVIRONMENT uint16 = 0x00000400
	CREATE_NO_WINDOW                  = 0x08000000
	CREATE_NEW_CONSOLE                = 0x00000010
)

//获得当前系统活动的SessionID
func GetCurrentUserSessionId() (windows.Handle, error) {
	sessionList, err := WTSEnumerateSessions()
	if err != nil {
		return 0xFFFFFFFF, fmt.Errorf("get current user session token: %s", err)
	}
	for i := range sessionList {
		if sessionList[i].State == WTSActive {
			return sessionList[i].SessionID, nil
		}
	}
	if sessionId, _, err := procWTSGetActiveConsoleSessionId.Call(); sessionId == 0xFFFFFFFF {
		return 0xFFFFFFFF, fmt.Errorf("get current user session token: call native WTSGetActiveConsoleSessionId: %s", err)
	} else {
		return windows.Handle(sessionId), nil
	}
}

// WTSEnumerateSession will call the native
// version for Windows and parse the result
// to a Golang friendly version
func WTSEnumerateSessions() ([]*WTS_SESSION_INFO, error) {
	var (
		sessionInformation windows.Handle      = windows.Handle(0)
		sessionCount       int                 = 0
		sessionList        []*WTS_SESSION_INFO = make([]*WTS_SESSION_INFO, 0)
	)
	if returnCode, _, err := procWTSEnumerateSessionsW.Call(WTS_CURRENT_SERVER_HANDLE, 0, 1, uintptr(unsafe.Pointer(&sessionInformation)), uintptr(unsafe.Pointer(&sessionCount))); returnCode == 0 {
		return nil, fmt.Errorf("call native WTSEnumerateSessionsW: %s", err)
	}
	structSize := unsafe.Sizeof(WTS_SESSION_INFO{})
	current := uintptr(sessionInformation)
	for i := 0; i < sessionCount; i++ {
		sessionList = append(sessionList, (*WTS_SESSION_INFO)(unsafe.Pointer(current)))
		current += structSize
	}
	return sessionList, nil
}

// DuplicateUserTokenFromSessionID will attempt
// to duplicate the user token for the user logged
// into the provided session ID
func DuplicateUserTokenFromSessionID(sessionId windows.Handle, runas bool) (windows.Token, error) {
	var (
		impersonationToken windows.Handle = 0
		userToken          windows.Token  = 0
	)

	if returnCode, _, err := procWTSQueryUserToken.Call(uintptr(sessionId), uintptr(unsafe.Pointer(&impersonationToken))); returnCode == 0 {
		return 0xFFFFFFFF, fmt.Errorf("call native WTSQueryUserToken: %s", err)
	}

	if returnCode, _, err := procDuplicateTokenEx.Call(uintptr(impersonationToken), 0, 0, uintptr(SecurityImpersonation), uintptr(TokenPrimary), uintptr(unsafe.Pointer(&userToken))); returnCode == 0 {
		return 0xFFFFFFFF, fmt.Errorf("call native DuplicateTokenEx: %s", err)
	}
	if runas {
		var admin TOKEN_LINKED_TOKEN
		var dt uintptr = 0
		if returnCode, _, _ := procGetTokenInformation.Call(uintptr(impersonationToken), 19, uintptr(unsafe.Pointer(&admin)), uintptr(unsafe.Sizeof(admin)), uintptr(unsafe.Pointer(&dt))); returnCode != 0 {
			userToken = admin.LinkedToken
		}
	}
	if err := windows.CloseHandle(impersonationToken); err != nil {
		return 0xFFFFFFFF, fmt.Errorf("close windows handle used for token duplication: %s", err)
	}
	return userToken, nil
}

//StartProcessAsCurrentUser(程序路径, 启动参数, 工作目录 string, 是否以管理员身份运行) error
//需要注意的是，若使用cmdLine传入启动参数，则需要加上传入文件路径，否则可能会有不可预期的错误。
//例：
//
//StartProcessAsCurrentUser(`C:\test\test.exe`,`C:\test\test.exe hello world`,`C:\test`,true)
func StartProcessAsCurrentUser(appPath, cmdLine, workDir string, runas bool) error {
	var (
		sessionId windows.Handle
		userToken windows.Token
		envInfo   windows.Handle

		startupInfo windows.StartupInfo
		processInfo windows.ProcessInformation

		commandLine uintptr = 0
		workingDir  uintptr = 0

		err error
	)

	if sessionId, err = GetCurrentUserSessionId(); err != nil {
		return err
	}

	if userToken, err = DuplicateUserTokenFromSessionID(sessionId, runas); err != nil {
		return fmt.Errorf("get duplicate user token for current user session: %s", err)
	}

	if returnCode, _, err := procCreateEnvironmentBlock.Call(uintptr(unsafe.Pointer(&envInfo)), uintptr(userToken), 0); returnCode == 0 {
		return fmt.Errorf("create environment details for process: %s", err)
	}

	creationFlags := CREATE_UNICODE_ENVIRONMENT | CREATE_NEW_CONSOLE
	startupInfo.ShowWindow = SW_SHOW
	startupInfo.Desktop = windows.StringToUTF16Ptr("winsta0\\default")

	if len(cmdLine) > 0 {
		commandLine = uintptr(unsafe.Pointer(windows.StringToUTF16Ptr(cmdLine)))
	}
	if len(workDir) > 0 {
		workingDir = uintptr(unsafe.Pointer(windows.StringToUTF16Ptr(workDir)))
	}
	if returnCode, _, err := procCreateProcessAsUser.Call(
		uintptr(userToken), uintptr(unsafe.Pointer(windows.StringToUTF16Ptr(appPath))), commandLine, 0, 0, 0,
		uintptr(creationFlags), uintptr(envInfo), workingDir, uintptr(unsafe.Pointer(&startupInfo)), uintptr(unsafe.Pointer(&processInfo)),
	); returnCode == 0 {
		return fmt.Errorf("create process as user: %s", err)
	}
	return nil
}
