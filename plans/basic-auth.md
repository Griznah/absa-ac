# Implementation Plan: Basic Auth with .htpasswd (Option 2)

## Overview

Implement HTTP Basic Authentication in Go using bcrypt-compatible
`.htpasswd` files. Intended for internal admin endpoints protected
behind TLS.

## Goals

-   Validate users via `.htpasswd`-style file
-   Support multiple users with bcrypt hashes
-   Keep code minimal and dependency-light
-   Fail closed on bad credentials

## Components

1.  **Password Storage**

    -   Use standard `.htpasswd` format
    -   Accept bcrypt (`$2a$`, `$2b$`, `$2y$`)
    -   Ignore legacy MD5/crypt hashes for security

2.  **Loader**

    -   Parse file into memory (map\[user\]hash)
    -   Skip comments and empty lines
    -   No reload logic (optional future enhancement)

3.  **Authentication Middleware**

    -   Extract credentials via `r.BasicAuth()`
    -   Compare bcrypt hash using `bcrypt.CompareHashAndPassword`
    -   Return `401` + `WWW-Authenticate` on failure
    -   Call next handler on success

4.  **Usage Pattern**

    ``` go
    mux.Handle("/admin",
        basicAuthHtpasswd(users, adminHandler),
    )
    ```

5.  **Password Creation** Use the following example to generate bcrypt
    hashes:

    ``` bash
    htpasswd -nbB admin strongpassword
    ```

6.  **Operational Considerations**

    -   Must run behind HTTPS
    -   Good for internal admin endpoints
    -   Not suitable for public user auth

## Testing

-   Test valid user → 200 OK
-   Test invalid password → 401 Unauthorized
-   Test no auth header → 401 Unauthorized
-   Test malformed `.htpasswd` entries → fatal or error on startup

