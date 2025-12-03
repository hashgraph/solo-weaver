// SPDX-License-Identifier: Apache-2.0

//go:build !darwin && !windows

package principal

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"
)

type lineReader[E unixUser | unixGroup] func(index int, line string) (*E, error)

func readEntityFile[E unixUser | unixGroup](file string, fn lineReader[E]) ([]*E, error) {
	entities := make([]*E, 0)

	fh, err := os.Open(file)
	if err != nil {
		return nil, err
	}

	defer fh.Close()

	scanner := bufio.NewScanner(fh)
	scanner.Split(bufio.ScanLines)

	index := 0
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		index++

		if len(line) == 0 {
			continue
		}

		if strings.HasPrefix(line, "#") {
			continue
		}

		ent, err := fn(index, line)
		if err != nil {
			return nil, err
		}

		if ent != nil {
			entities = append(entities, ent)
		}
	}

	return entities, nil
}

func parseUnixUser(index int, line string) (*unixUser, error) {
	if !strings.Contains(line, ":") {
		return nil, fmt.Errorf("invalid user entry at line %d, no colons were present", index)
	}

	parts := strings.Split(line, ":")
	if len(parts) != 7 {
		return nil, fmt.Errorf("invalid user entry at line %d, not enough fields", index)
	}

	// The unix passwd file has the following colon delimited fields:
	// username:password:uid:gid:gecos:home:shell
	// parts[0] = username
	// parts[1] = password
	// parts[2] = uid
	// parts[3] = gid
	// parts[4] = gecos
	// parts[5] = home
	// parts[6] = shell

	username := strings.TrimSpace(parts[0])
	if len(username) == 0 {
		return nil, fmt.Errorf("invalid user entry at line %d, empty username field", index)
	}

	uid, err := strconv.Atoi(parts[2])
	if err != nil {
		return nil, fmt.Errorf("invalid user entry at line %d, invalid uid field", index)
	}

	gid, err := strconv.Atoi(parts[3])
	if err != nil {
		return nil, fmt.Errorf("invalid user entry at line %d, invalid gid field", index)
	}

	displayName := displayNameFromGecos(parts[4])
	homeDir := strings.TrimSpace(parts[5])
	shell := strings.TrimSpace(parts[6])

	return &unixUser{
		name:        username,
		uid:         uid,
		gid:         gid,
		displayName: displayName,
		homeDir:     homeDir,
		shell:       shell,
	}, nil
}

func parseUnixGroup(index int, line string) (*unixGroup, error) {

	// The unix group file has the following colon delimited fields:
	// groupname:password:gid:members
	// parts[0] = groupname
	// parts[1] = password
	// parts[2] = gid
	// parts[3] = members

	if !strings.Contains(line, ":") {
		return nil, fmt.Errorf("invalid group entry at line %d, no colons were present", index)
	}

	parts := strings.Split(line, ":")
	if len(parts) < 3 || len(parts) > 4 {
		return nil, fmt.Errorf("invalid group entry at line %d, not enough fields", index)
	}

	groupname := strings.TrimSpace(parts[0])
	if len(groupname) == 0 {
		return nil, fmt.Errorf("invalid group entry at line %d, empty groupname field", index)
	}

	gid, err := strconv.Atoi(parts[2])
	if err != nil {
		return nil, fmt.Errorf("invalid group entry at line %d, invalid gid field", index)
	}

	var members []string
	if len(parts) == 4 {
		members = parseMembers(parts[3])
	} else {
		// users who have this group as primary group in /etc/passwd will not be listed in the members field
		members = make([]string, 0)
	}

	return &unixGroup{
		name:    groupname,
		gid:     gid,
		members: members,
	}, nil
}

func displayNameFromGecos(gecos string) string {
	if !strings.Contains(gecos, ",") {
		return strings.TrimSpace(gecos)
	}

	parts := strings.Split(gecos, ",")
	if len(parts) == 0 {
		return gecos
	}

	return strings.TrimSpace(parts[0])
}

func parseMembers(members string) []string {
	if len(strings.TrimSpace(members)) == 0 {
		return make([]string, 0)
	}

	list := strings.Split(members, ",")
	for i, m := range list {
		list[i] = strings.TrimSpace(m)
	}

	return list
}
