package principal

import (
	"bufio"
	"bytes"
	"fmt"
	"github.com/joomcode/errorx"
	"howett.net/plist"
	"os/exec"
	"strconv"
	"strings"
)

const (
	shellCmd             = "/bin/bash"
	directoryServicesCmd = "/usr/bin/dscl"
	dsclEntityTypeUser   = "users"
	dsclEntityTypeGroup  = "groups"
)

type dsclUserInfo struct {
	UniqueID       []string `plist:"dsAttrTypeStandard:UniqueID"`
	PrimaryGroupID []string `plist:"dsAttrTypeStandard:PrimaryGroupID"`
	RealName       []string `plist:"dsAttrTypeStandard:RealName"`
	HomeDir        []string `plist:"dsAttrTypeStandard:NFSHomeDirectory"`
	Shell          []string `plist:"dsAttrTypeStandard:UserShell"`
}

type dsclGroupInfo struct {
	UniqueID []string `plist:"dsAttrTypeStandard:PrimaryGroupID"`
	Members  []string `plist:"dsAttrTypeStandard:GroupMembership"`
}

func dsclEnumerateUsers() ([]*unixUser, *errorx.Error) {
	userNames, err := dsclEnumerateEntities(dsclEntityTypeUser)
	if err != nil {
		return nil, err
	}

	users := make([]*unixUser, len(userNames))
	for i, userName := range userNames {
		user, err := dsclGetUserInfo(userName)
		if err != nil {
			return nil, err
		}

		uid, e := strconv.Atoi(user.UniqueID[0])
		if e != nil {
			return nil, errorx.IllegalFormat.New("Invalid UID for user %s: %v", userName, e).WithUnderlyingErrors(e)
		}

		gid, e := strconv.Atoi(user.PrimaryGroupID[0])
		if e != nil {
			return nil, errorx.IllegalFormat.New("Invalid GID for user %s: %v", userName, e).WithUnderlyingErrors(e)
		}

		users[i] = &unixUser{
			name:        userName,
			displayName: user.RealName[0],
			uid:         uid,
			gid:         gid,
			homeDir:     user.HomeDir[0],
			shell:       user.Shell[0],
		}
	}

	return users, nil
}

func dsclEnumerateGroups() ([]*unixGroup, *errorx.Error) {
	groupNames, err := dsclEnumerateEntities(dsclEntityTypeGroup)

	if err != nil {
		return nil, err
	}

	groups := make([]*unixGroup, len(groupNames))
	for i, groupName := range groupNames {
		group, err := dsclGetGroupInfo(groupName)
		if err != nil {
			return nil, err
		}

		gid, e := strconv.Atoi(group.UniqueID[0])
		if e != nil {
			return nil, errorx.IllegalFormat.New("Invalid GID for group %s", groupName).WithUnderlyingErrors(e)
		}

		if group.Members == nil {
			group.Members = make([]string, 0)
		}

		groups[i] = &unixGroup{
			name:    groupName,
			gid:     gid,
			members: group.Members,
		}
	}

	return groups, nil
}

func dsclEnumerateEntities(entityType string) ([]string, *errorx.Error) {
	command := &exec.Cmd{
		Path: directoryServicesCmd,
		Args: []string{
			directoryServicesCmd,
			".",
			"list",
			fmt.Sprintf("/%s", entityType),
		},
	}

	output, e := command.Output()
	if e != nil {
		return nil, errorx.IllegalState.Wrap(e, "Failed to execute dscl command")
	}

	scanner := bufio.NewScanner(bytes.NewReader(output))
	scanner.Split(bufio.ScanLines)

	list := make([]string, 0)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if len(line) == 0 {
			continue
		}

		if strings.Contains(line, "*errorx.Error: DS") {
			return nil, errorx.IllegalState.New("dscl Error: %s", line)
		}

		if strings.HasPrefix(line, "#") {
			continue
		}

		list = append(list, line)
	}

	return list, nil
}

func dsclGetUserInfo(name string) (*dsclUserInfo, *errorx.Error) {
	output, err := dsclGetEntityInfo(dsclEntityTypeUser, name)
	if err != nil {
		return nil, err
	}

	var info dsclUserInfo
	if _, e := plist.Unmarshal(output, &info); e != nil {
		return nil, errorx.IllegalFormat.New("Failed to unmarshal dscl user info for %s", name).WithUnderlyingErrors(e)
	}

	return &info, nil
}

func dsclGetGroupInfo(name string) (*dsclGroupInfo, *errorx.Error) {
	output, err := dsclGetEntityInfo(dsclEntityTypeGroup, name)
	if err != nil {
		return nil, err
	}

	var info dsclGroupInfo
	if _, e := plist.Unmarshal(output, &info); e != nil {
		return nil, errorx.IllegalFormat.New("Failed to unmarshal dscl group info for %s", name).WithUnderlyingErrors(e)
	}

	return &info, nil
}

func dsclGetEntityInfo(entityType string, name string) ([]byte, *errorx.Error) {
	command := &exec.Cmd{
		Path: directoryServicesCmd,
		Args: []string{
			directoryServicesCmd,
			"-plist",
			".",
			"read",
			fmt.Sprintf("/%s/%s", entityType, name),
		},
	}

	output, e := command.Output()
	if e != nil {
		return nil, errorx.IllegalState.Wrap(e, "Failed to execute dscl command")
	}

	return output, nil
}
