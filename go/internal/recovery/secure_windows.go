//go:build windows

package recovery

import (
	"fmt"
	"os"
	"syscall"
	"unsafe"
)

// The standard library exposes SID and token helpers but not ACL editing, so
// the minimal advapi32 entry points are bound directly. No third-party module
// is involved.
var (
	advapi32 = syscall.NewLazyDLL("advapi32.dll")
	k32acl   = syscall.NewLazyDLL("kernel32.dll")

	procInitializeAcl         = advapi32.NewProc("InitializeAcl")
	procAddAccessAllowedAceEx = advapi32.NewProc("AddAccessAllowedAceEx")
	procSetNamedSecurityInfoW = advapi32.NewProc("SetNamedSecurityInfoW")
	procGetNamedSecurityInfoW = advapi32.NewProc("GetNamedSecurityInfoW")
	procGetAclInformation     = advapi32.NewProc("GetAclInformation")
	procGetAce                = advapi32.NewProc("GetAce")
	procLocalFree             = k32acl.NewProc("LocalFree")
)

const (
	fileAllAccess             = 0x1F01FF
	aclRevision               = 2
	daclSecurityInformation   = 0x00000004
	protectedDACLSecurityInfo = 0x80000000
	seFileObject              = 1
	aclSizeInformationClass   = 2
	accessAllowedAceType      = 0
	objectInheritACE          = 0x1
	containerInheritACE       = 0x2
	maxSIDSize                = 68
)

type aclSizeInformation struct {
	AceCount      uint32
	AclBytesInUse uint32
	AclBytesFree  uint32
}

type aceHeader struct {
	AceType  byte
	AceFlags byte
	AceSize  uint16
}

// checkPrivateDir verifies the DACL without changing it.
func checkPrivateDir(path string) error { return verifyPrivateDACL(path) }

// secureDir creates the data root and installs a protected DACL granting only
// the current user, SYSTEM, and Administrators. Failure disables storage.
func secureDir(path string) error {
	if err := os.MkdirAll(path, 0o700); err != nil {
		return err
	}
	if err := setPrivateDACL(path); err != nil {
		return err
	}
	return verifyPrivateDACL(path)
}

func currentUserSID() (*syscall.SID, string, error) {
	token, err := syscall.OpenCurrentProcessToken()
	if err != nil {
		return nil, "", err
	}
	defer token.Close()
	u, err := token.GetTokenUser()
	if err != nil {
		return nil, "", err
	}
	buf := make([]byte, maxSIDSize)
	sid := (*syscall.SID)(unsafe.Pointer(&buf[0]))
	if err := syscall.CopySid(uint32(len(buf)), sid, u.User.Sid); err != nil {
		return nil, "", err
	}
	s, err := sid.String()
	if err != nil {
		return nil, "", err
	}
	return sid, s, nil
}

func setPrivateDACL(path string) error {
	userSID, _, err := currentUserSID()
	if err != nil {
		return fmt.Errorf("resolve current user SID: %w", err)
	}
	systemSID, err := syscall.StringToSid("S-1-5-18")
	if err != nil {
		return err
	}
	adminsSID, err := syscall.StringToSid("S-1-5-32-544")
	if err != nil {
		return err
	}

	acl := make([]byte, 512)
	r1, _, e1 := procInitializeAcl.Call(uintptr(unsafe.Pointer(&acl[0])), uintptr(len(acl)), aclRevision)
	if r1 == 0 {
		return fmt.Errorf("InitializeAcl: %v", e1)
	}
	for _, sid := range []*syscall.SID{userSID, systemSID, adminsSID} {
		r, _, e := procAddAccessAllowedAceEx.Call(
			uintptr(unsafe.Pointer(&acl[0])),
			aclRevision,
			objectInheritACE|containerInheritACE,
			fileAllAccess,
			uintptr(unsafe.Pointer(sid)),
		)
		if r == 0 {
			return fmt.Errorf("AddAccessAllowedAceEx: %v", e)
		}
	}

	pathPtr, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return err
	}
	r, _, e := procSetNamedSecurityInfoW.Call(
		uintptr(unsafe.Pointer(pathPtr)),
		seFileObject,
		daclSecurityInformation|protectedDACLSecurityInfo,
		0, 0,
		uintptr(unsafe.Pointer(&acl[0])),
		0,
	)
	if r != 0 {
		return fmt.Errorf("SetNamedSecurityInfoW: %v (code %d)", e, r)
	}
	return nil
}

func verifyPrivateDACL(path string) error {
	pathPtr, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return err
	}
	var sd, dacl unsafe.Pointer
	r, _, e := procGetNamedSecurityInfoW.Call(
		uintptr(unsafe.Pointer(pathPtr)),
		seFileObject,
		daclSecurityInformation,
		0, 0,
		uintptr(unsafe.Pointer(&dacl)),
		0,
		uintptr(unsafe.Pointer(&sd)),
	)
	if r != 0 {
		return fmt.Errorf("GetNamedSecurityInfoW: %v (code %d)", e, r)
	}
	defer procLocalFree.Call(uintptr(sd))
	if dacl == nil {
		return fmt.Errorf("privacy: data root has no DACL")
	}

	var info aclSizeInformation
	r, _, e = procGetAclInformation.Call(
		uintptr(dacl),
		uintptr(unsafe.Pointer(&info)),
		unsafe.Sizeof(info),
		aclSizeInformationClass,
	)
	if r == 0 {
		return fmt.Errorf("GetAclInformation: %v", e)
	}

	_, userSIDStr, err := currentUserSID()
	if err != nil {
		return err
	}
	allowed := map[string]bool{userSIDStr: true, "S-1-5-18": true, "S-1-5-32-544": true}
	for i := uint32(0); i < info.AceCount; i++ {
		var ace unsafe.Pointer
		r, _, e := procGetAce.Call(uintptr(dacl), uintptr(i), uintptr(unsafe.Pointer(&ace)))
		if r == 0 {
			return fmt.Errorf("GetAce(%d): %v", i, e)
		}
		hdr := (*aceHeader)(ace)
		if hdr.AceType != accessAllowedAceType {
			continue
		}
		sid := (*syscall.SID)(unsafe.Add(ace, 8))
		s, err := sid.String()
		if err != nil {
			return fmt.Errorf("read ACE SID: %w", err)
		}
		if !allowed[s] {
			return fmt.Errorf("privacy: data root DACL grants %s", s)
		}
	}
	return nil
}
