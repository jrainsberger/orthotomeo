// orthotomeo web UI - plain fetch + DOM, no framework (T27 decision).

// Must match the #corpus <select>'s <option> values in index.html exactly -
// these are the only corpora /interlinear and /concord accept
// (retriever.IsWordCorpus server-side). KJV/ASV/WEB/Brenton are verse-text
// editions, not word-tagged corpora, and a Citation from /verse or /passage
// carries one of those as its `edition` - refCell must not build an
// interlinear cross-link from one (found in real use: clicking a KJV
// citation's ref set the corpus <select> to "KJV", an option that doesn't
// exist, leaving it unselected and blank - the request then failed with
// "missing required query param \"corpus\"").
const WORD_CORPORA = new Set(["TAGNT", "TAHOT", "Swete", "OSS-LXX-lemma"]);

const MODES = {
  verse: {
    endpoint: "/verse",
    fields: ["book", "chapter", "verse", "editions"],
  },
  passage: {
    endpoint: "/passage",
    fields: ["book", "start_chapter", "start_verse", "end_chapter", "end_verse", "editions"],
  },
  concord: {
    endpoint: "/concord",
    fields: ["query", "corpus", "by", "phrase", "window"],
  },
  parse: {
    endpoint: "/parse",
    fields: ["book", "chapter", "verse", "word", "corpus"],
  },
  attest: {
    endpoint: "/attest",
    fields: ["book", "chapter", "verse", "word", "corpus"],
  },
  interlinear: {
    endpoint: "/interlinear",
    fields: ["book", "chapter", "verse", "word", "corpus"],
  },
  define: {
    endpoint: "/define",
    fields: ["dstrong"],
  },
};

const modeSelect = document.getElementById("mode");
const statusEl = document.getElementById("status");
const resultsEl = document.getElementById("results");
const sourcesEl = document.getElementById("sources");
const backLinkEl = document.getElementById("backLink");

// --- book autocomplete ---------------------------------------------------
//
// The book field stays a free-text input (not a <select>) - this is a
// single fluent user's tool, and forcing a dropdown would slow down typing
// a code someone already knows. A <datalist> adds suggestions and
// typo-catching without giving up that speed; unlike a <select> it doesn't
// strictly enforce a valid choice, which is the accepted trade-off.
// Populated from the real canonical registry (GET /books, backed by the
// same books.json every loader treats as ground truth) rather than a
// hand-typed <option> list in index.html, so it can't drift from the real
// 66-book list.
fetch("/books")
  .then((res) => res.json())
  .then((list) => {
    const datalist = document.getElementById("books");
    for (const b of list) {
      datalist.appendChild(el("option", { value: b.code, text: `${b.code} — ${b.name}` }));
    }
  })
  .catch(() => {
    /* autocomplete is a nice-to-have; a failed fetch just leaves the field
       as a plain text input, still fully usable. */
  });

// --- form <-> params -------------------------------------------------

// fieldValue reads one named field regardless of its control type -
// checkboxes (editions: several boxes sharing one name, gather every
// CHECKED one - a plain click on a native <select multiple> silently
// deselects everything else, which is why this isn't a multi-select) or a
// plain input/select.
function fieldValue(mode, name) {
  const checkboxes = document.querySelectorAll(`[data-field="${name}"] input[type="checkbox"][name="${name}"]`);
  if (checkboxes.length > 0) {
    return Array.from(checkboxes)
      .filter((c) => c.checked)
      .map((c) => c.value)
      .join(",");
  }
  const input = document.querySelector(`[data-field="${name}"] [name="${name}"]`);
  return input ? input.value : "";
}

// setFieldValue is fieldValue's inverse - used both to pre-fill a
// cross-link's target field and to restore a field when navigating back.
function setFieldValue(name, value) {
  const checkboxes = document.querySelectorAll(`[data-field="${name}"] input[type="checkbox"][name="${name}"]`);
  if (checkboxes.length > 0) {
    const values = new Set(value ? value.split(",") : []);
    for (const c of checkboxes) c.checked = values.has(c.value);
    return;
  }
  const input = document.querySelector(`[data-field="${name}"] [name="${name}"]`);
  if (input) input.value = value || "";
}

function gatherParams(mode) {
  const params = new URLSearchParams();
  for (const name of MODES[mode].fields) {
    const v = fieldValue(mode, name);
    if (v !== "") params.set(name, v);
  }
  return params;
}

function applyParams(mode, params) {
  for (const name of MODES[mode].fields) {
    setFieldValue(name, params.get(name) || "");
  }
}

// --- search + history --------------------------------------------------

// historyStack holds one entry per cross-link navigation (not every
// search - only pushed when a cross-link jumps you to a different mode),
// so "back" undoes "I clicked into a definition/concordance/interlinear
// view from a result row," not a full undo-every-search log.
const historyStack = [];

function updateBackLink() {
  backLinkEl.style.display = historyStack.length > 0 ? "inline" : "none";
}

async function runSearch(mode) {
  const params = gatherParams(mode);
  statusEl.textContent = "loading...";
  statusEl.classList.remove("error");
  resultsEl.innerHTML = "";
  sourcesEl.innerHTML = "";
  hideTooltip();

  try {
    const res = await fetch(`${MODES[mode].endpoint}?${params.toString()}`);
    const body = await res.json();
    if (!res.ok) {
      statusEl.textContent = body.error || `request failed (${res.status})`;
      statusEl.classList.add("error");
      return;
    }
    statusEl.textContent = "";
    render(mode, body);
  } catch (err) {
    statusEl.textContent = String(err);
    statusEl.classList.add("error");
  }
}

// crossLinkTo jumps to another mode with specific field values pre-filled
// (e.g. a lemma cell -> concord mode, query=that lemma) and runs the
// search immediately - the whole point is landing on the answer, not a
// pre-filled form waiting for another click. Pushes the CURRENT mode/params
// onto historyStack first, so "back" can undo the jump.
function crossLinkTo(mode, fieldValues) {
  historyStack.push({ mode: modeSelect.value, params: gatherParams(modeSelect.value) });
  updateBackLink();

  modeSelect.value = mode;
  showFieldsFor(mode);
  for (const [name, value] of Object.entries(fieldValues)) {
    setFieldValue(name, value);
  }
  runSearch(mode);
}

backLinkEl.addEventListener("click", (ev) => {
  ev.preventDefault();
  const prev = historyStack.pop();
  if (!prev) return;
  updateBackLink();
  modeSelect.value = prev.mode;
  showFieldsFor(prev.mode);
  applyParams(prev.mode, prev.params);
  runSearch(prev.mode);
});

function showFieldsFor(mode) {
  for (const el of document.querySelectorAll(".field")) {
    el.classList.toggle("active", MODES[mode].fields.includes(el.dataset.field));
  }
}

modeSelect.addEventListener("change", () => showFieldsFor(modeSelect.value));
showFieldsFor(modeSelect.value);

document.getElementById("search").addEventListener("submit", (ev) => {
  ev.preventDefault();
  runSearch(modeSelect.value);
});

function render(mode, body) {
  if (mode === "define") {
    renderEntry(body);
    return;
  }
  if (mode === "interlinear") {
    renderWords(body.words || []);
    renderSources(body.sources || {});
    return;
  }
  renderCitations(body.citations || []);
  renderSources(body.sources || {});
}

function el(tag, attrs, children) {
  const n = document.createElement(tag);
  for (const [k, v] of Object.entries(attrs || {})) {
    if (k === "text") n.textContent = v;
    else if (k === "html") n.innerHTML = v;
    else n.setAttribute(k, v);
  }
  for (const c of children || []) n.appendChild(c);
  return n;
}

// xlink is a cross-link cell: styled and keyboard-focusable like a real
// link (not a div with an onclick - tab order and Enter-to-activate come
// free), navigating to another mode with context carried over instead of
// requiring the reader to retype what they're already looking at.
function xlink(text, onActivate) {
  const a = el("a", { href: "#", class: "xlink", text });
  a.addEventListener("click", (ev) => {
    ev.preventDefault();
    onActivate();
  });
  return a;
}

function confidenceBadge(confidence, caveat) {
  const cls = confidence === "High" ? "high" : "flagged";
  const title =
    confidence === "High"
      ? "High confidence: a direct, unambiguous match - no alignment guesswork involved."
      : caveat || "Flagged: this result involved an inexact alignment or a gap in the source data - see the note below it.";
  return el("span", { class: `badge ${cls}`, text: confidence, title });
}

// ATTESTATION_TITLE gives a plain-language gloss for the Type letters
// (N=Nestle-Aland, K=KJV-source/Scrivener, O=other editions) so "KO" isn't
// opaque shorthand with no explanation anywhere in the UI.
function attestationTitle(attestation) {
  if (!attestation) return "";
  const known = { N: "Nestle-Aland", K: "KJV-source (Scrivener)", O: "other editions" };
  const letters = attestation.replace(/[()]/g, "").split("");
  const parts = letters.map((l) => known[l.toUpperCase()] || l);
  return `Type ${attestation}: attested in ${parts.join(", ")}.`;
}

// superscriptNumber renders a row index as a footnote-style ordinal mark
// (¹, ², ..., ¹⁰, ...) - the citation number IS a footnote reference in a
// critical edition, not a generic "row 1, row 2" index, so it's styled and
// spelled that way rather than as a plain digit in its own column.
const SUPERSCRIPT_DIGITS = { 0: "⁰", 1: "¹", 2: "²", 3: "³", 4: "⁴", 5: "⁵", 6: "⁶", 7: "⁷", 8: "⁸", 9: "⁹" };
function superscriptNumber(n) {
  return String(n)
    .split("")
    .map((d) => SUPERSCRIPT_DIGITS[d] ?? d)
    .join("");
}
function ordinalCell(n) {
  return el("td", { class: "ordinal", "aria-label": `row ${n}`, text: superscriptNumber(n) });
}

function origCell(text, translit) {
  // dir="auto" (not a hardcoded "rtl") lets the browser's own bidi
  // algorithm pick direction per cell, since the same column holds LTR
  // Greek and RTL Hebrew depending on corpus - this is the one piece of
  // the whole UI that requires zero special-casing to render either
  // correctly, the reason a browser beats a native GUI toolkit here.
  const td = el("td", { class: "orig", dir: "auto" }, [
    el("div", { class: "orig-text", text: text || "" }),
  ]);
  if (translit) td.appendChild(el("div", { class: "translit", text: translit }));
  return td;
}

function refCell(ref, corpus) {
  const text = `${ref.book}.${ref.chapter}.${ref.verse}`;
  if (!corpus || !WORD_CORPORA.has(corpus)) return el("td", { text });
  return el("td", {}, [
    xlink(text, () => crossLinkTo("interlinear", { book: ref.book, chapter: String(ref.chapter), verse: String(ref.verse), corpus })),
  ]);
}

function lemmaCell(lemma, corpus) {
  if (!lemma) return el("td", {});
  if (!corpus) return el("td", { dir: "auto", text: lemma });
  return el("td", { dir: "auto" }, [xlink(lemma, () => crossLinkTo("concord", { query: lemma, corpus, by: "lemma" }))]);
}

function dstrongCell(dstrong) {
  if (!dstrong) return el("td", {});
  const a = xlink(dstrong, () => {
    hideTooltip();
    crossLinkTo("define", { dstrong });
  });
  a.addEventListener("mouseenter", () => scheduleGlossPreview(a, dstrong));
  a.addEventListener("focus", () => scheduleGlossPreview(a, dstrong));
  a.addEventListener("mouseleave", cancelGlossPreview);
  a.addEventListener("blur", cancelGlossPreview);
  return el("td", {}, [a]);
}

// --- hover gloss preview -------------------------------------------------
//
// A dStrong link's click-through (define mode) already shows the full
// gloss/definition/translit - the preview is a lighter-weight "what does
// this mean" for someone skimming many rows without leaving the results,
// not a replacement for the full view. Debounced (a fast mouse pass over
// several cells shouldn't fire a fetch per cell) and cached per dStrong (a
// repeated hover never re-fetches).

const definitionCache = new Map();
let hoverTimer = null;
let tooltipEl = null;

function ensureTooltip() {
  if (!tooltipEl) {
    tooltipEl = el("div", { class: "gloss-tooltip", role: "status", "aria-live": "polite" });
    document.body.appendChild(tooltipEl);
  }
  return tooltipEl;
}

function hideTooltip() {
  if (tooltipEl) tooltipEl.style.display = "none";
}

function positionTooltip(target) {
  const tip = ensureTooltip();
  const rect = target.getBoundingClientRect();
  tip.style.left = `${rect.left + window.scrollX}px`;
  tip.style.top = `${rect.bottom + window.scrollY + 6}px`;
}

function cancelGlossPreview() {
  clearTimeout(hoverTimer);
  hideTooltip();
}

function scheduleGlossPreview(target, dstrong) {
  clearTimeout(hoverTimer);
  hoverTimer = setTimeout(() => fetchGlossPreview(target, dstrong), 250);
}

async function fetchGlossPreview(target, dstrong) {
  const tip = ensureTooltip();
  positionTooltip(target);

  if (definitionCache.has(dstrong)) {
    tip.textContent = definitionCache.get(dstrong);
    tip.style.display = "block";
    return;
  }

  tip.textContent = "…";
  tip.style.display = "block";
  try {
    const res = await fetch(`/define?dstrong=${encodeURIComponent(dstrong)}`);
    if (!res.ok) {
      hideTooltip();
      return;
    }
    const entry = await res.json();
    const text = entry.translit ? `${entry.gloss} (${entry.translit})` : entry.gloss;
    definitionCache.set(dstrong, text);
    // the hover may have already moved on by the time this resolves
    if (tooltipEl.style.display === "block") tooltipEl.textContent = text;
  } catch {
    hideTooltip();
  }
}

// --- render --------------------------------------------------------------

function renderCitations(cs) {
  if (cs.length === 0) {
    resultsEl.appendChild(el("p", { text: "No results." }));
    return;
  }
  const table = el("table", {}, [
    el("thead", {}, [
      el("tr", {}, [
        el("th", { "aria-label": "row marker" }),
        el("th", { text: "ref" }),
        el("th", { text: "edition" }),
        el("th", { text: "text" }),
        el("th", { text: "lemma" }),
        el("th", { text: "dstrong" }),
        el("th", { text: "grammar" }),
        el("th", { text: "type" }),
        el("th", { text: "manuscripts" }),
        el("th", { text: "confidence" }),
      ]),
    ]),
  ]);
  const tbody = el("tbody", {}, []);
  cs.forEach((c, i) => {
    const row = el("tr", {}, [
      ordinalCell(i + 1),
      refCell(c.ref, c.edition),
      el("td", { text: c.edition }),
      origCell(c.text, c.translit),
      lemmaCell(c.lemma, c.edition),
      dstrongCell(c.dstrong),
      el("td", { text: c.grammar || "" }),
      el("td", { title: attestationTitle(c.attestation) }, [document.createTextNode(c.attestation || "")]),
      el("td", { class: "mono-cell", text: c.manuscripts || "" }),
      el("td", {}, [confidenceBadge(c.confidence, c.caveat)]),
    ]);
    tbody.appendChild(row);
    if (c.caveat) {
      const cav = el("tr", {}, [el("td", { colspan: "10", class: "caveat", text: c.caveat })]);
      tbody.appendChild(cav);
    }
  });
  table.appendChild(tbody);
  resultsEl.appendChild(table);
}

function renderWords(words) {
  if (words.length === 0) {
    resultsEl.appendChild(el("p", { text: "No results." }));
    return;
  }
  const table = el("table", {}, [
    el("thead", {}, [
      el("tr", {}, [
        el("th", { "aria-label": "row marker" }),
        el("th", { text: "ref" }),
        el("th", { text: "text" }),
        el("th", { text: "lemma" }),
        el("th", { text: "dstrong" }),
        el("th", { text: "gloss" }),
        el("th", { text: "grammar" }),
        el("th", { text: "confidence" }),
      ]),
    ]),
  ]);
  const tbody = el("tbody", {}, []);
  words.forEach((w, i) => {
    const row = el("tr", {}, [
      ordinalCell(i + 1),
      refCell(w.ref, w.edition),
      origCell(w.text, w.translit),
      lemmaCell(w.lemma, w.edition),
      dstrongCell(w.dstrong),
      el("td", { text: w.gloss || "" }),
      el("td", { text: w.grammar || "" }),
      el("td", {}, [confidenceBadge(w.confidence, w.caveat)]),
    ]);
    tbody.appendChild(row);
    if (w.caveat) {
      const cav = el("tr", {}, [el("td", { colspan: "8", class: "caveat", text: w.caveat })]);
      tbody.appendChild(cav);
    }
  });
  table.appendChild(tbody);
  resultsEl.appendChild(table);
}

function renderEntry(e) {
  const heading = el("div", { class: "entry-heading" }, [
    el("span", { class: "entry-dstrong", text: e.dstrong }),
    el("span", { class: "entry-lemma", dir: "auto", text: e.lemma || "" }),
  ]);
  if (e.translit) heading.appendChild(el("span", { class: "entry-translit", text: e.translit }));

  const card = el("div", { class: "entry-card" }, [
    heading,
    el("div", { class: "entry-gloss", text: e.gloss || "" }),
  ]);
  if (e.definition) {
    // innerHTML, not textContent, deliberately: this string only ever
    // comes from our own bundled, static lexicon data (Abbott-Smith via
    // STEPBible's TBESG file) - never a request parameter or any other
    // user-controllable input - and the source data itself embeds real
    // markup (<b> headwords, <BR/> line breaks) meant to render. No
    // injection surface here to sanitize against.
    card.appendChild(el("div", { class: "entry-definition", html: e.definition }));
  } else {
    card.appendChild(
      el("div", { class: "caveat" }, [
        document.createTextNode(
          "No fuller definition for this entry - Hebrew definitions are withheld pending permission (T34); only gloss is ever returned for those."
        ),
      ])
    );
  }
  resultsEl.appendChild(card);
}

function renderSources(sources) {
  const codes = Object.keys(sources);
  if (codes.length === 0) return;
  sourcesEl.appendChild(el("h2", { text: "colophon" }));
  for (const code of codes) {
    const s = sources[code];
    const row = el("div", { class: "source-row" }, [
      el("span", { class: "code", text: code }),
      el("span", { class: "path", text: s.file }),
    ]);
    if (s.homepage_url) {
      row.appendChild(el("a", { href: s.homepage_url, target: "_blank", rel: "noopener", text: "source" }));
    }
    if (s.license) {
      row.appendChild(el("span", { class: "badge", text: s.license }));
    }
    sourcesEl.appendChild(row);
  }
}

document.getElementById("printBtn").addEventListener("click", () => window.print());
