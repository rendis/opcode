# Social Cross-Post

Post the same content to Twitter, LinkedIn, and a blog API simultaneously. Each platform posts independently with on_error ignore, so one failure does not block the others.

## Inputs

| Param | Type | Required | Description |
|-------|------|----------|-------------|
| content | string | yes | Text content to post across platforms |
| title | string | no | Post title (used for blog and LinkedIn) |
| twitter_api_url | string | yes | Twitter/X API endpoint |
| linkedin_api_url | string | yes | LinkedIn API endpoint |
| blog_api_url | string | yes | Blog publishing API endpoint |

## Steps

`cross-post` (parallel: `post-twitter` + `post-linkedin` + `post-blog`) -> `log-summary`

## Features

- Parallel mode posts to all three platforms simultaneously
- on_error ignore on each branch so individual platform failures are tolerated
- Retry with exponential backoff on each HTTP POST
- Summary log after all posts complete
