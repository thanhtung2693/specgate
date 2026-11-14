const tl = gsap.timeline({ paused: true });

function swapHeadline(from, to, note, time) {
  tl.to(from, { y: -80, opacity: 0, duration: 0.28, ease: "power3.in" }, time);
  tl.fromTo(to, { y: 80, opacity: 0 }, { y: 0, opacity: 1, duration: 0.42, ease: "power4.out" }, time + 0.16);
  tl.to("#headline-note", { opacity: 0, duration: 0.18 }, time);
  tl.set("#headline-note", { textContent: note }, time + 0.2);
  tl.to("#headline-note", { opacity: 1, duration: 0.28, ease: "sine.out" }, time + 0.2);
}

function swapAction(from, to, time) {
  tl.to(from, { y: -17, opacity: 0, duration: 0.2, ease: "power2.in" }, time);
  tl.fromTo(to, { y: 17, opacity: 0 }, { y: 0, opacity: 1, duration: 0.26, ease: "power3.out" }, time + 0.15);
}

function setNode(node, label, color, time) {
  tl.to(`${node} .node-dot`, { backgroundColor: color, boxShadow: `0 0 0 2px ${color}`, duration: 0.22 }, time);
  tl.to(node, { color, duration: 0.22 }, time);
  tl.set(`${node} .node-result`, { textContent: label, color }, time);
}

function setSurface(label, color, time) {
  tl.set("#surface-state-copy", { textContent: label, color }, time);
  tl.to(".state-dot", { backgroundColor: color, boxShadow: `0 0 18px ${color}`, duration: 0.22 }, time);
}

function advanceRail(distance, color, time) {
  tl.to(".lane-runner", { x: distance, backgroundColor: color, duration: 0.62, ease: "power2.inOut" }, time);
  tl.to("#lane-progress", { scaleX: distance / 1225, stroke: color, duration: 0.62, ease: "power2.inOut" }, time);
}

const amber = "#f0ae46";
const green = "#4bd178";
const indigo = "#8993ff";

// The surface stays in place as the evidence changes.
tl.fromTo(".product-bar", { y: -24, opacity: 0 }, { y: 0, opacity: 1, duration: 0.38, ease: "expo.out" }, 0.1);
tl.fromTo(".eyebrow", { x: -34, opacity: 0 }, { x: 0, opacity: 1, duration: 0.28, ease: "power3.out" }, 0.16);
tl.fromTo("#headline-claim", { y: 72, opacity: 0 }, { y: 0, opacity: 1, duration: 0.48, ease: "power4.out" }, 0.22);
tl.fromTo("#headline-note", { x: -28, opacity: 0 }, { x: 0, opacity: 1, duration: 0.34, ease: "sine.out" }, 0.5);
tl.fromTo(".proof-surface", { y: 64, scale: 0.975, opacity: 0 }, { y: 0, scale: 1, opacity: 1, duration: 0.55, ease: "expo.out" }, 0.28);
tl.fromTo(".surface-top > *", { y: 14, opacity: 0 }, { y: 0, opacity: 1, duration: 0.25, stagger: 0.08, ease: "power2.out" }, 0.62);
tl.fromTo(".proof-node", { y: 18, opacity: 0 }, { y: 0, opacity: 1, duration: 0.3, stagger: 0.09, ease: "power3.out" }, 0.68);
tl.fromTo(".terminal-strip", { y: 74, opacity: 0 }, { y: 0, opacity: 1, duration: 0.34, ease: "power3.out" }, 0.85);
tl.set("#lane-progress", { scaleX: 0 }, 0);

swapHeadline("#headline-claim", "#headline-gap", "The suite ran, but the agreed 503 testcase is absent.", 2.15);
swapAction("#action-submit", "#action-check", 2.4);
advanceRail(408, amber, 2.55);
setNode(".node-gap", "Selected test missing", amber, 3.14);
setSurface("Evidence gap", amber, 3.14);
tl.fromTo(".evidence-gap", { x: 44, opacity: 0 }, { x: 0, opacity: 1, duration: 0.4, ease: "expo.out" }, 3.15);

swapHeadline("#headline-gap", "#headline-rerun", "The coding agent adds the missing case and reruns the unit checks.", 5.95);
swapAction("#action-check", "#action-rerun", 6.15);
tl.to(".evidence-gap", { opacity: 0, duration: 0.33, ease: "sine.inOut" }, 7.06);
advanceRail(816, green, 6.75);
setNode(".node-gap", "Evidence recorded", green, 7.37);
setNode(".node-rerun", "Selected test passed", green, 7.37);
setSurface("Ready for human review", green, 7.4);
tl.fromTo(".revision-proof", { y: 28, opacity: 0 }, { y: 0, opacity: 1, duration: 0.46, ease: "power4.out" }, 7.46);

swapHeadline("#headline-rerun", "#headline-ready", "Read the new test and its result, then decide.", 9.85);
advanceRail(1225, indigo, 10.15);
setNode(".node-human", "Awaiting acceptance", indigo, 10.78);
setSurface("Awaiting human acceptance", indigo, 10.78);

swapHeadline("#headline-ready", "#headline-delivered", "SpecGate records your decision with its evidence.", 12.48);
swapAction("#action-rerun", "#action-accept", 12.5);
setSurface("Human accepted", green, 12.95);
tl.to([".proof-lane", ".finding-stage"], { opacity: 0, duration: 0.26, ease: "power2.in" }, 12.95);
tl.fromTo(".delivered-state", { scale: 0.98, opacity: 0 }, { scale: 1, opacity: 1, duration: 0.46, ease: "power3.out" }, 12.95);
tl.fromTo(".delivered-mark", { scale: 0.58, rotation: -12 }, { scale: 1, rotation: 0, duration: 0.42, ease: "expo.out" }, 13.13);
tl.fromTo(".delivered-state > div:last-child", { x: 24, opacity: 0 }, { x: 0, opacity: 1, duration: 0.34, ease: "power3.out" }, 13.18);
tl.fromTo(".delivered-orbit", { scale: 0.72, opacity: 0 }, { scale: 1, opacity: 1, duration: 0.5, ease: "sine.out" }, 13.12);

window.__specgatePromoTimeline = tl;
