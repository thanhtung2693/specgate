/* Safe reading of pinned Markdown; source bytes remain separate from presentation. */
(function (root, factory) {
  if (typeof module !== "undefined" && module.exports)
    module.exports = factory(
      require("./assets/vendor/marked/marked.umd.js"),
      require("./core.js").escape,
    );
  else root.SpecGateMarkdown = factory(root.marked, root.SpecGateUI.escape);
})(globalThis, function (marked, escape) {
  "use strict";
  function httpURL(value) {
    try {
      const url = new URL(value);
      return url.protocol === "http:" || url.protocol === "https:"
        ? url.href
        : null;
    } catch {
      return null;
    }
  }
  // Marked emits its standard tags and escapes text/code. Override every sink
  // that can otherwise emit source HTML, a destination attribute, or an image.
  class ReaderRenderer extends marked.Renderer {
    html({ text }) {
      return escape(text);
    }
    link({ href, tokens, title }) {
      const label = this.parser.parseInline(tokens),
        url = httpURL(href);
      return url
        ? `<a href="${escape(url)}"${title ? ` title="${escape(title)}"` : ""} target="_blank" rel="noopener noreferrer">${label}</a>`
        : label;
    }
    image({ text }) {
      return `<span class="markdown-image-alt">${escape(text)}</span>`;
    }
    table(token) {
      return `<div class="markdown-table">${super.table(token)}</div>`;
    }
  }
  const lex = (source) => marked.Lexer.lex(source, { gfm: true });
  const parse = (tokens) =>
    marked.Parser.parse(tokens, { gfm: true, renderer: new ReaderRenderer() });
  const renderMarkdown = (source) => parse(lex(source));
  function renderContext(
    source,
    { generatedQuick = false, workRef = "" } = {},
  ) {
    const tokens = lex(source);
    let index = -1;
    if (generatedQuick && workRef) {
      const expected = [
        [1, `Implementation Context Pack: ${workRef}`],
        [2, "Quick Handoff"],
        [3, "Intent"],
        [3, "Acceptance criteria"],
        [3, "Impact declaration"],
        [3, "Policy snapshot"],
      ];
      const headings = tokens.filter(
        (token) => token.type === "heading" && token.depth <= 3,
      );
      if (
        headings.length === expected.length &&
        headings.every(
          (heading, i) =>
            heading.depth === expected[i][0] && heading.text === expected[i][1],
        )
      )
        index = tokens.indexOf(headings[4]);
    }
    return {
      source,
      html: parse(index < 0 ? tokens : tokens.slice(0, index)),
      technicalHTML: index < 0 ? "" : parse(tokens.slice(index)),
    };
  }
  return { renderMarkdown, renderContext };
});
