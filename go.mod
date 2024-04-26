module github.com/chamzzzzzz/mail-archiver

go 1.22.0

require github.com/emersion/go-imap/v2 v2.0.0-beta.2

require (
	github.com/emersion/go-message v0.18.0 // indirect
	github.com/emersion/go-sasl v0.0.0-20231106173351-e73c9f7bad43 // indirect
	github.com/emersion/go-textwrapper v0.0.0-20200911093747-65d896831594 // indirect
	golang.org/x/text v0.14.0 // indirect
)

replace github.com/emersion/go-imap/v2 => github.com/x0st/go-imap/v2 v2.0.0-20240320130701-f9ce23121ff0
