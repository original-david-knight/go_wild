package tools

// PublishSiteInput defines the input for publishing a static website.
type PublishSiteInput struct {
	Slug      string `json:"slug" description:"URL slug for the site (3-40 chars, lowercase letters, numbers and hyphens only, e.g. 'my-app')" required:"true"`
	Title     string `json:"title" description:"Display name for the site" required:"true"`
	Directory string `json:"directory" description:"Path to directory containing site files (must include index.html, e.g. /data/mysite/)" required:"true"`
}

// ListSitesInput defines the input for listing published sites.
type ListSitesInput struct{}
