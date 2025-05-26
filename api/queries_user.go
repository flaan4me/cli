package api

type Organization struct {
	Login string
}

func CurrentLoginName(client *Client, hostname string) (string, error) {
	var query struct {
		Viewer struct {
			Login string
		}
	}
	err := client.Query(hostname, "UserCurrent", &query, nil)
	return query.Viewer.Login, err
}

func CurrentLoginNameAndOrgs(client *Client, hostname string) (string, []string, error) {
	var query struct {
		Viewer struct {
			Login         string
			Organizations struct {
				Nodes []Organization
			} `graphql:"organizations(first: 100)"`
		}
	}
	err := client.Query(hostname, "UserCurrent", &query, nil)
	if err != nil {
		return "", nil, err
	}
	orgNames := []string{}
	for _, org := range query.Viewer.Organizations.Nodes {
		orgNames = append(orgNames, org.Login)
	}
	return query.Viewer.Login, orgNames, err
}

func CurrentUserID(client *Client, hostname string) (string, error) {
	var query struct {
		Viewer struct {
			ID string
		}
	}
	err := client.Query(hostname, "UserCurrent", &query, nil)
	return query.Viewer.ID, err
}

func GetUser(client *Client, login string) (*User, error) {
	var result struct {
		User *User
	}
	query := `
	query($login: String!) {
		user(login: $login) {
			id
			login
			name
			bio
			company
			location
			email
			websiteUrl
			createdAt
			updatedAt
			followers {
				totalCount
			}
			following {
				totalCount
			}
			repositories {
				totalCount
			}
			gists {
				totalCount
			}
			organizations {
				totalCount
			}
			starredRepositories {
				totalCount
			}
			watching {
				totalCount
			}
			issues {
				totalCount
			}
			pullRequests {
				totalCount
			}
			projects {
				totalCount
			}
			packages {
				totalCount
			}
			securityAdvisories {
				totalCount
			}
			sponsorshipsAsMaintainer {
				totalCount
			}
			sponsorshipsAsSponsor {
				totalCount
			}
			status {
				emoji
				message
			}
		}
	}`
	variables := map[string]interface{}{
		"login": login,
	}
	err := client.GraphQL(ghinstance.Default(), query, variables, &result)
	if err != nil {
		return nil, err
	}
	return result.User, nil
}
