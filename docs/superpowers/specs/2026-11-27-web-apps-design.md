# Web apps in a computer

## Why

An agent in a computer writes a web page or a small Python app into a folder. The host (appbox first) should be
able to show it with no registration step and no process to run. PR #116 proved a Flask app runs in the embedded
Python; it is a proof of concept and is not built on.

## Goals

- A file is reached by its path and an app by its standard name, `module:callable`, in the path: nothing is
  registered or discovered.
- Static pages cost no Python.
- A Flask page answers in tens of milliseconds, with nothing for the agent to install.
- The host keeps listening, TLS, sign-in, limits and timeouts; computer adds none of them.

## Not now

- ASGI, FastAPI, WebSockets: asyncio and threads do not run in the embedded Python.
- State in the app's memory between requests: it lives in files or SQLite.
- Apps as first-class appbox apps (publish, install, their own MCP tools, a member per request).

## Approaches

1. **A kept-alive Python worker per app** (PR #116). 6 ms a request, but requests are serial, a hung request
   takes the app's memory with it, each live app holds a 512 MiB run, and it needs worker lifecycle code and a
   private wire format.
2. **A fresh Python per request, as CGI, importing Flask each time.** No lifecycle code, but about 600 ms a
   request, nearly all of it the import.
3. **A fresh Python per request, as CGI, with Flask in the start image.** Chosen. Measured at 25 to 38 ms a
   request. Requests run in parallel up to the Python's MaxRuns, a hung one is stopped alone, nothing is held
   while idle, and the wire is the standard one, which `wsgiref` already speaks.

## Design

One model: the computer's files have URLs, as `python3 -m http.server` gives a folder's files URLs, and a
Python application has one too, its standard name. The host mounts one handler and declares nothing.

1. **`WebHandler(get)`**, shaped as `TerminalHandler` is: `get` returns the requester's computer and a folder.
   The path is a path under the folder: a file is sent as it is, a folder as its `index.html` or its list, by
   `http.FileServer`. A path part written `module:callable`, as gunicorn and uvicorn name an application, is that
   application, where the module's file is, and the rest of the path is the application's (CGI's SCRIPT_NAME and
   PATH_INFO): `/shop/app:app/cart`. `/shop/app.py` stays the file. Only a module that is a file there is run.
2. Inside it, unexported until a host needs one alone: the computer's files as an `fs.FS` for
   `http.FileServer`; a function that runs a shell script of the computer as a CGI program (RFC 3875), the way
   `net/http/cgi` runs one, with the request's body read first; and the one-line use of Python's own `wsgiref`
   that serves a WSGI application (PEP 3333) over it. An `ErrorLog` says where an application's error output goes.
3. **Flask in the start image.** The module carries Flask and its pure-Python dependencies as `python/site.zip`,
   whose files are added to the release's own zip of packages as it is unpacked, so no mount or path is new, and
   the code that makes the image imports Flask when the release has it.
4. **`computer serve [-b address] [-d folder] [port]`**, the handler on a local address, with `http.server`'s
   flags.

What a later kind of application needs is a name in the path and a script for `CGI`: an ASGI application is
named the same way, once the embedded Python runs asyncio.

Considered and dropped: finding apps by a marker file (`app.py` in a folder), running whatever Python file the
URL names, and having the host declare each application in Go. Each made the library guess, or the host
register, what the URL can simply say.

## Left to the builder

Names of unexported things, the exact CGI variables beyond RFC 3875's, and how much of a failed script's error
output reaches the host's log.
