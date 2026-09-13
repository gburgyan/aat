"""Publish the AI assistant primer as raw Markdown at the site root.

The primer's source is internal/primer/llms.md, which docs/user/llms.md includes
as a page and `aat docs primer` prints. A tool that fetches a web page may
summarize it rather than return it verbatim, so the build also writes the
Markdown itself to llms-full.txt. The llms.txt index is a static file in
docs/user. Keep this compatible with Python 3.9.
"""

import os
import shutil

from mkdocs.exceptions import PluginError

PRIMER = os.path.join("internal", "primer", "llms.md")


def on_post_build(config, **kwargs):
    root = os.path.dirname(os.path.abspath(config["config_file_path"]))
    source = os.path.join(root, PRIMER)
    if not os.path.isfile(source):
        raise PluginError("llms-full.txt: primer source %s not found" % source)
    shutil.copyfile(source, os.path.join(config["site_dir"], "llms-full.txt"))
