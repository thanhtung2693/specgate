/* SpecGate landing - interactions: theme toggle, scroll reveals, verdict console. */

/* ---------- Theme toggle ---------- */
const themeToggle = document.querySelector(".theme-toggle");

function paintToggle(theme) {
  if (!themeToggle) return;
  const isLight = theme === "light";
  themeToggle.setAttribute("aria-pressed", String(isLight));
  const label = themeToggle.querySelector(".theme-label");
  if (label) label.textContent = isLight ? "Dark" : "Light";
}

paintToggle(document.documentElement.dataset.theme || "dark");

themeToggle?.addEventListener("click", () => {
  const next = (document.documentElement.dataset.theme || "dark") === "light" ? "dark" : "light";
  document.documentElement.dataset.theme = next;
  localStorage.setItem("specgate-landing-theme", next);
  paintToggle(next);
});

/* ---------- Scroll reveals ---------- */
const revealObserver = new IntersectionObserver(
  (entries, observer) => {
    entries.forEach((entry) => {
      if (entry.isIntersecting) {
        entry.target.classList.add("in");
        observer.unobserve(entry.target);
      }
    });
  },
  { rootMargin: "0px 0px -10% 0px", threshold: 0.15 },
);

document.querySelectorAll(".reveal-up").forEach((el) => revealObserver.observe(el));

/* ---------- Verdict console ---------- */
const chip = document.querySelector("#verdict-chip");
const checks = Array.from(document.querySelectorAll("#check-list li"));
const probes = Array.from(document.querySelectorAll("#console-checks .chk"));
const replayBtn = document.querySelector("#console-replay");

let consoleTimers = [];
const prefersReducedMotion = matchMedia("(prefers-reduced-motion: reduce)").matches;

function resetConsole() {
  consoleTimers.forEach(clearTimeout);
  consoleTimers = [];
  checks.forEach((li) => li.setAttribute("data-status", "pending"));
  probes.forEach((p) => p.setAttribute("data-on", "0"));
  if (chip) {
    chip.setAttribute("data-state", "run");
    chip.textContent = "Verifying…";
  }
}

function runConsole() {
  resetConsole();
  if (prefersReducedMotion) {
    checks.forEach((li) => li.setAttribute("data-status", "met"));
    probes.forEach((p) => p.setAttribute("data-on", "1"));
    if (chip) {
      chip.setAttribute("data-state", "pass");
      chip.textContent = "Pass 4/4";
    }
    return;
  }
  const stepIn = (fn, delay) => consoleTimers.push(setTimeout(fn, delay));
  let t = 520;
  checks.forEach((li, i) => {
    stepIn(() => li.setAttribute("data-status", "met"), t + i * 620);
  });
  t += checks.length * 620 + 180;
  probes.forEach((p, i) => {
    stepIn(() => p.setAttribute("data-on", "1"), t + i * 260);
  });
  t += probes.length * 260 + 240;
  stepIn(() => {
    if (chip) {
      chip.setAttribute("data-state", "pass");
      chip.textContent = "Pass 4/4";
    }
  }, t);
}

replayBtn?.addEventListener("click", runConsole);

if (chip && checks.length) {
  // Kick off once the console scrolls into view.
  const consoleEl = document.querySelector(".console");
  if (consoleEl) {
    const once = new IntersectionObserver(
      (entries, observer) => {
        entries.forEach((entry) => {
          if (entry.isIntersecting) {
            runConsole();
            observer.disconnect();
          }
        });
      },
      { threshold: 0.4 },
    );
    once.observe(consoleEl);
  }
}

/* ---------- Scrollspy nav ---------- */
const navLinks = Array.from(document.querySelectorAll(".nav-links a"));
const spyTargets = navLinks
  .map((a) => {
    const id = a.getAttribute("href")?.slice(1);
    const section = id && document.getElementById(id);
    return section ? { a, section } : null;
  })
  .filter(Boolean);

if (spyTargets.length) {
  const spy = new IntersectionObserver(
    (entries) => {
      entries.forEach((entry) => {
        if (!entry.isIntersecting) return;
        const match = spyTargets.find((t) => t.section === entry.target);
        if (!match) return;
        navLinks.forEach((a) => a.classList.remove("active"));
        match.a.classList.add("active");
      });
    },
    { rootMargin: "-45% 0px -50% 0px" },
  );
  spyTargets.forEach((t) => spy.observe(t.section));
}

/* ---------- See-it-run: feature-demo carousel (Raycast-style) ---------- */
(function initCastCarousel() {
  const root = document.querySelector("[data-carousel]");
  if (!root) return;

  const CASTS = [
    {
      title: "specgate / your shell",
      cap: { t: "Drive it from your shell.", d: "check the board, pull the governed pack, report evidence, and read the verdict." },
      lines: [
        { k: "cmd", t: "$ specgate status --all-workspaces" },
        { k: "out", t: "  Work: 18 total (ready 1, handoff 17)" },
        { k: "out", t: "  Ready: 1  Needs attention: 0" },
        { k: "cmd", t: "$ specgate work list --all-workspaces" },
        { k: "out", t: "  No work items need attention." },
        { k: "cmd", t: "$ specgate work show CR-1D0256D8" },
        { k: "out", t: "  Phase: Handoff" },
        { k: "out", t: "  Context pack: specgate://context-pack/d6425c9b..." },
        { k: "cmd", t: "$ specgate work context CR-1D0256D8 > context-pack.md" },
        { k: "ok", t: "  wrote the approved implementation Context Pack" },
        { k: "cmd", t: "$ specgate delivery report CR-1D0256D8 --init" },
        { k: "out", t: "  Wrote completion.json for CR-1D0256D8 (1 acceptance criteria)." },
        { k: "cmd", t: "$ specgate delivery submit CR-1D0256D8 --file completion.json" },
        { k: "pass", t: "  report → gates → review → status" },
      ],
    },
    {
      title: "cursor / claude code / codex",
      cap: { t: "Hand off to your IDE agent.", d: "the agent reads a governed Context Pack before it touches code." },
      lines: [
        { k: "dim", t: "# in Cursor, Claude Code, or Codex" },
        { k: "cmd", t: "$ specgate work context \"$WORK_REF\" --json" },
        { k: "out", t: "  state=\"assembled\"  context_pack_uri=\"specgate://context-pack/...\"" },
        { k: "cmd", t: "# read approved spec, non-goals, risks, ACs" },
        { k: "out", t: "  rule: approved artifact outranks chat and tracker text" },
        { k: "cmd", t: "# implement only the Context Pack scope" },
        { k: "cmd", t: "# run tests, types, lint; attach changed files" },
        { k: "cmd", t: "$ specgate delivery submit \"$WORK_REF\" --file completion.json --json" },
        { k: "pass", t: "  ok=true  command=\"delivery.submit\"" },
      ],
    },
    {
      title: "specgate / readiness gates",
      cap: { t: "Run gates when needed.", d: "quality gates can be triggered and inspected from the same CLI surface." },
      lines: [
        { k: "cmd", t: "$ specgate gates status CR-1D0256D8" },
        { k: "out", t: "  No gate runs found." },
        { k: "cmd", t: "$ specgate gates run CR-1D0256D8 --json" },
        { k: "dim", t: "  runs server-side LLM quality gates when a model is configured" },
        { k: "cmd", t: "$ specgate gates history CR-1D0256D8" },
        { k: "ok", t: "  gate history becomes the durable readiness ledger" },
      ],
    },
    {
      title: "specgate / delivery review",
      cap: { t: "Proof after the build.", d: "delivery review maps every claim back to acceptance criteria and checks." },
      lines: [
        { k: "cmd", t: "$ specgate delivery report CR-1D0256D8 --init" },
        { k: "out", t: "  Fill in: summary, affected_files, checks, and per-criterion claim" },
        { k: "cmd", t: "$ specgate delivery submit CR-1D0256D8 --file completion.json" },
        { k: "dim", t: "  one command runs report, gates, review, then status" },
        { k: "cmd", t: "$ specgate delivery status CR-1D0256D8 --detail --json" },
        { k: "out", t: "  {\"found\":false} until the first review is recorded" },
      ],
    },
    {
      title: "specgate / stats",
      cap: { t: "Know it's working.", d: "one readout of what governance caught, computed from real gate runs and reviews." },
      lines: [
        { k: "cmd", t: "$ specgate stats" },
        { k: "out", t: "  reviewed 12 work items / first-pass yield 83% (10/12)" },
        { k: "out", t: "  caught pre-build 4 / post-build 3 (3 fixed before merge)" },
        { k: "out", t: "  ambiguity saves 2 / avg cycle 6.2h create → pass" },
        { k: "dim", t: "  caught by SpecGate (recent)" },
        { k: "ok", t: "  ✓ WI-097  review_catch  unverified AC claim → fixed before merge" },
        { k: "pass", t: "  and when governance isn't earning its keep, it says so" },
      ],
    },
    {
      title: "specgate / governance profile",
      cap: { t: "Bring your own gates.", d: "profiles bind required roles, quality gates, and team Skills to each change type." },
      lines: [
        { k: "dim", t: "# profile.yaml - bind gates + Skills" },
        { k: "cmd", t: "gates:" },
        { k: "out", t: "  - key: threat_model_present" },
        { k: "out", t: "    rubric: skill://security/threat-review" },
        { k: "out", t: "  - key: pii_logging_check" },
        { k: "out", t: "    required_for: high_impact_feature" },
        { k: "cmd", t: "$ specgate gates run WI-204 --json" },
        { k: "ok", t: "  ✓ threat_model_present  custom Skill applied" },
        { k: "ok", t: "  ✓ pii_logging_check     not_applicable with evidence" },
        { k: "pass", t: "  gates  GREEN / profile enforced before handoff" },
      ],
    },
  ];

  const segmentList = root.querySelector("[data-cast-segments]");
  const segmentLabels = ["CLI loop", "IDE agent", "Run gates", "Delivery review", "Stats", "Custom gates"];
  if (!segmentList) return;

  segmentList.replaceChildren(
    ...CASTS.map((_, i) => {
      const item = document.createElement("li");
      const button = document.createElement("button");
      const fill = document.createElement("span");
      button.type = "button";
      button.role = "tab";
      button.dataset.i = String(i);
      button.setAttribute("aria-selected", String(i === 0));
      button.setAttribute("aria-label", segmentLabels[i] || `Demo ${i + 1}`);
      fill.className = "seg-fill";
      button.appendChild(fill);
      item.appendChild(button);
      return item;
    }),
  );

  const segs = Array.from(segmentList.querySelectorAll("button"));
  const titleEl = root.querySelector("[data-cast-title]");
  const body = root.querySelector("[data-cast-body]");
  const capEl = root.querySelector("[data-cast-caption]");
  const N = CASTS.length;

  let timers = [];
  let raf = 0;
  let current = 0;
  let runId = 0;

  const clear = () => {
    timers.forEach(clearTimeout);
    timers = [];
    if (raf) cancelAnimationFrame(raf);
    raf = 0;
  };

  function buildLines(cast) {
    body.replaceChildren();
    return cast.lines.map((ln) => {
      const span = document.createElement("span");
      span.className = "tl show";
      span.dataset.k = ln.k;
      body.appendChild(span);
      body.appendChild(document.createTextNode("\n"));
      return { ...ln, span };
    });
  }

  function estimateDuration(cast) {
    let d = 220;
    cast.lines.forEach((l) => {
      d += l.k === "cmd" ? l.t.length * 12 + 220 : 240;
    });
    return d + 2400; // trailing hold before advance
  }

  function setActive(idx) {
    const cap = CASTS[idx].cap;
    titleEl.textContent = CASTS[idx].title;
    capEl.style.opacity = "0";
    timers.push(
      setTimeout(() => {
        const title = document.createElement("b");
        title.textContent = cap.t;
        const detail = document.createElement("span");
        detail.textContent = cap.d;
        capEl.replaceChildren(title, document.createTextNode(" "), detail);
        capEl.style.opacity = "1";
      }, 180),
    );
    segs.forEach((b, i) => {
      b.setAttribute("aria-selected", String(i === idx));
      b.dataset.done = i < idx ? "1" : "0";
      const f = b.querySelector(".seg-fill");
      f.style.transition = "none";
      f.style.width = i < idx ? "100%" : "0%";
    });
  }

  function runSegFill(idx, ms) {
    const f = segs[idx]?.querySelector(".seg-fill");
    if (!f) return;
    raf = requestAnimationFrame(() => {
      f.style.transition = "width " + ms + "ms linear";
      f.style.width = "100%";
    });
  }

  function play(idx) {
    clear();
    const token = ++runId;
    current = idx;
    setActive(idx);
    const cast = CASTS[idx];
    const lines = buildLines(cast);

    if (prefersReducedMotion) {
      lines.forEach((l) => (l.span.textContent = l.t));
      const f = segs[idx]?.querySelector(".seg-fill");
      if (f) f.style.width = "100%";
      return;
    }

    runSegFill(idx, estimateDuration(cast));

    let li = 0;
    const next = () => {
      if (token !== runId) return;
      if (li >= lines.length) {
        timers.push(
          setTimeout(() => {
            if (token === runId) play((idx + 1) % N);
          }, 2400),
        );
        return;
      }
      const l = lines[li++];
      if (l.k === "cmd") {
        l.span.classList.add("cur");
        let c = 0;
        const type = () => {
          if (token !== runId) return;
          l.span.textContent = l.t.slice(0, c);
          if (c < l.t.length) {
            c++;
            timers.push(setTimeout(type, 12));
          } else {
            l.span.classList.remove("cur");
            timers.push(setTimeout(next, 220));
          }
        };
        type();
      } else {
        l.span.textContent = l.t;
        timers.push(setTimeout(next, 240));
      }
    };
    timers.push(setTimeout(next, 220));
  }

  segs.forEach((b, i) => b.addEventListener("click", () => play(i)));

  if (prefersReducedMotion) {
    play(0);
    return;
  }
  const io = new IntersectionObserver(
    (entries, observer) => {
      entries.forEach((entry) => {
        if (entry.isIntersecting) {
          play(0);
          observer.disconnect();
        }
      });
    },
    { threshold: 0.4 },
  );
  io.observe(root);
})();
