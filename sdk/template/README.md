# {{NAME}}

A [Dalibre](https://github.com/DavidSche/dalibre) book source: it shows up in Dalibre's Get books page.

```sh
dalibre-plugin check .                         # validate plugin.json and main.js
dalibre-plugin search . "dune"                 # run search as Dalibre would
dalibre-plugin get . "dune"                    # what the detail panel shows
dalibre-plugin download . "dune" --format EPUB # resolve and download the first result
dalibre-plugin pack .                          # make {{ID}}-<version>.dalibre-plugin
```

Pass settings with `--set apiKey=…`.
