# markdown-webgen

Golang based Markdown website generator.

## SYNOPSIS

markdown-webgen [-destdir dir] [-assetdir dir] [-quiet] mdfile [mdfile ...]

## DESCRIPTION

This program reads a given markdown file, collects other linked markdown files,
converts them all to html using a template, and replaces the markdown links
to point to the corresponding html files.

The options are as follows:

 * -destdir dir - Destination directory to write all resulting files to, defaults to the same directory as the source file

 * -assetdir - Directory with additional files to copy to the destination directory, unset by default.

 * -quiet - Avoid printing detailed output
