# Public

Any file placed in here will be accessible via the http server.

## ZIP applications

If a ZIP file is placed in this folder that contains an, index.html the contents
of the zip file is served directly from the zip folder. The name of the zipfile
then represents the foldername in the http path (without the .zip).

## Fauxton

The ZIP mechanism is used for the `_utils.zip` fauxton admin ui.
The admin ui will be available at `/_utils/` and serve the contents of the zip.
