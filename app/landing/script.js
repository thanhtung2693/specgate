"use strict";

const strings = {
  en: {
    "cap.0": "The agent says done. Three criteria, one word.", "cap.1": "The suite passed. The 503 case still has no result.", "cap.2": "Accept or request changes for the delivery you reviewed.",
    "pp.pause": "Pause", "pp.play": "Play", "copy.done": "Copied", "copy.fail": "Copy failed",
  },
  vi: {
    skip: "Đến nội dung chính", "brand.aria": "Trang chủ SpecGate", "lang.aria": "Ngôn ngữ",
    "theme.aria": "Đổi giao diện sáng hoặc tối", "steps.aria": "Các bước minh họa",
    "hero.h1": "Agent báo xong. <em>Xong những gì?</em>", "hero.lede": "CLI chạy trên máy bạn. Xem bằng chứng theo từng yêu cầu trước khi duyệt.",
    "cta.demo": "Thử UI demo", "cta.install": "Bắt đầu với CLI",
    "w.said": "Agent báo đạt 3/3", "w.sum": "1 trong 3 chưa có bằng chứng", "r.miss": "Thiếu kết quả test",
    "r.claim": "Báo cáo", "r.source": "Nguồn", "r.check": "Lệnh", "r.test": "Test", "c.fix": "Gửi yêu cầu sửa", "c.ok": "Duyệt bản này",
    "st.0": "Agent báo xong", "st.1": "Soi từng tiêu chí", "st.2": "Bạn quyết định",
    "cap.0": "Agent báo xong. Vậy ba tiêu chí đã đạt chưa?", "cap.1": "Test xanh, nhưng ca 503 vẫn chưa có kết quả.", "cap.2": "Duyệt hoặc yêu cầu sửa đúng bản giao bạn vừa xem.",
    "pp.pause": "Tạm dừng", "pp.play": "Chạy", "pp.replay": "Xem lại", "copy.copy": "Sao chép", "copy.done": "Đã chép", "copy.fail": "Chép thất bại",
    "fit.h": "Công cụ quen cứ dùng. SpecGate lo khâu đối chiếu.",
    "fit.note": "Dùng khi dễ sót yêu cầu hoặc cần nối việc ở phiên khác. Sửa lỗi chính tả hay đổi nhãn thì agent và test như thường là đủ.",
    "flow.scope.h": "Chốt yêu cầu", "flow.scope.link": "Bắt đầu với Quick Work",
    "flow.agent.h": "Agent làm", "flow.agent.link": "Tiếp tục đúng việc",
    "flow.evidence.h": "Kiểm chứng", "flow.evidence.p": "Xem bằng chứng cho từng yêu cầu và biết mục nào còn thiếu.", "flow.evidence.link": "Test đã chọn",
    "flow.decision.h": "Duyệt và chuyển giao", "flow.decision.p": "Duyệt hoặc trả lại, trên đúng bản giao đó.", "flow.decision.link": "Chuyển kho dữ liệu Local",
    "ins.h": "Bắt đầu với CLI.", "ins.aria": "Cách cài đặt", "ins.tabPrompt": "Nhờ agent",
    "ins.prompt": "Tạo Quick Work trong SpecGate cho thay đổi này.\nVới mỗi mục có thể test, cho tôi xem tiêu chí, @check:tên đề xuất và lệnh test cụ thể.\nChờ tôi duyệt trước khi tạo work, sửa code hoặc chạy bất kỳ test nào.\nSau khi duyệt, dùng đúng các liên kết này, triển khai, nộp bằng chứng rồi dừng để tôi xem.",
    "ins.codex": "Trong Codex, mở <code>/plugins</code> rồi cài <code>specgate@specgate</code>.",
    "ins.cursor": "Chạy cả hai lệnh trong repo Git để cài và kiểm tra cấu hình cho dự án này.",
    "ins.first": "Cài CLI trước. Mỗi IDE chỉ dùng một cách cài plugin, rồi khởi động lại IDE.", "ins.ide": "Hướng dẫn cài plugin",
    "ins.then": "CLI miễn phí, chạy cục bộ với agent bạn đang dùng. Muốn duyệt trên trình duyệt thì chạy <code>specgate ui</code>.", "ins.link": "Hướng dẫn Quick Work",
    "faq.h": "Hỏi nhanh", "faq.1q": "Test xanh vẫn có thể sót yêu cầu?", "faq.1a": "Có. Cả bộ test có thể xanh dù ca 503 đã chốt vẫn chưa có kết quả.",
    "faq.2q": "SpecGate kiểm tra những gì?", "faq.2a": "Bạn thấy agent báo gì, lệnh nào đã chạy và kết quả của các test đã chọn nếu có. Test đạt vẫn có thể sót hành vi bạn cần. Bạn xem code và quyết định bằng chứng đã đủ chưa.",
    "faq.3q": "Có bắt buộc dùng trình duyệt không?", "faq.3a": "CLI Local ghi lại phạm vi, bằng chứng và quyết định. Nếu muốn xem và duyệt qua trình duyệt, chạy <code>specgate ui</code>.",
    "faq.demo": " với dữ liệu mẫu.", "faq.guide": "Xem cách duyệt trên repo của bạn",
    "fin.h": "Thử với một thay đổi nhỏ xem.", "foot.aria": "Cuối trang", "foot.docs": "Tài liệu",
  },
};

const root = document.documentElement;
const select = (selector) => document.querySelector(selector);
const all = (selector) => [...document.querySelectorAll(selector)];

function readPreference(key) {
  try { return localStorage.getItem(key); } catch { return null; }
}

function savePreference(key, value) {
  try { localStorage.setItem(key, value); } catch { /* Preferences are optional. */ }
}

const languageButtons = all("[data-lang]");
const translations = all("[data-i18n], [data-i18n-attr]").map((element) => {
  const [names, key] = element.dataset.i18nAttr?.split(":") ?? [];
  const attributes = names?.split(",");
  return {
    element,
    attributes,
    key: element.dataset.i18n ?? key,
    english: attributes ? element.getAttribute(attributes[0]) : element.innerHTML,
  };
});

const theme = select('[data-action="theme"]');
theme.addEventListener("click", () => {
  root.dataset.theme = root.dataset.theme === "dark" ? "light" : "dark";
  savePreference("specgate-landing-theme", root.dataset.theme);
});

const controls = select(".ctl-bar");
const scene = select("#scene");
const steps = all("[data-step]");
const caption = select("#cap");
const player = select("#pp");
const playerIcon = select("#pp use");
const reducedMotion = matchMedia("(prefers-reduced-motion: reduce)");
const holds = [3600, 4400, 1600];
let at = 2;
let playing = false;
let timer;
let remaining = 0;
let started = 0;
let pausedAnimations = [];

function string(key) {
  return strings[root.lang]?.[key] ?? strings.en[key];
}

function updatePlayer() {
  caption.textContent = string(`cap.${at}`);
  playerIcon.setAttribute("href", playing ? "#i-pause" : "#i-play");
  player.setAttribute("aria-label", string(playing ? "pp.pause" : "pp.play"));
}

function setLanguage(language) {
  root.lang = language === "vi" ? "vi" : "en";
  for (const { element, attributes, key, english } of translations) {
    const value = string(key) ?? english;
    if (attributes) for (const attribute of attributes) element.setAttribute(attribute, value);
    else element.innerHTML = value;
  }
  for (const button of languageButtons) button.setAttribute("aria-pressed", String(button.dataset.lang === root.lang));
  updatePlayer();
}

function showPhase(index) {
  for (const animation of pausedAnimations) animation.finish();
  pausedAnimations = [];
  at = index;
  remaining = holds[index] ?? 0;
  scene.dataset.phase = String(index);
  scene.setAttribute("aria-labelledby", steps[index].id);
  scene.classList.remove("go");
  if (index === 0) {
    // Restart the entrance even when replay begins on the first step.
    void scene.offsetWidth;
    scene.classList.add("go");
  }
  for (const [position, step] of steps.entries()) {
    step.setAttribute("aria-selected", String(position === index));
    step.tabIndex = position === index ? 0 : -1;
  }
  updatePlayer();
}

function stopClock() {
  clearTimeout(timer);
  playing = false;
  updatePlayer();
}

function pause() {
  if (playing) remaining = Math.max(0, remaining - (performance.now() - started));
  stopClock();
  pausedAnimations = scene.getAnimations({ subtree: true }).filter((animation) => animation.playState === "running");
  for (const animation of pausedAnimations) animation.pause();
}

function schedule() {
  started = performance.now();
  timer = setTimeout(() => {
    if (at === 2) {
      remaining = 0;
      stopClock();
      return;
    }
    showPhase(at + 1);
    schedule();
  }, remaining);
}

function play(restart) {
  stopClock();
  if (restart || (at === 2 && remaining === 0)) showPhase(0);
  for (const animation of pausedAnimations) animation.play();
  pausedAnimations = [];
  playing = true;
  updatePlayer();
  schedule();
}

player.addEventListener("click", () => {
  if (playing) pause();
  else play(false);
});
select("#replay").addEventListener("click", () => play(true));
const stepList = select("#steps");
stepList.addEventListener("focusin", () => { if (playing) pause(); });
stepList.addEventListener("click", (event) => {
  const step = event.target.closest("[data-step]");
  if (!step) return;
  stopClock();
  showPhase(Number(step.dataset.step));
});
stepList.addEventListener("keydown", (event) => {
  const next = { ArrowRight: (at + 1) % steps.length, ArrowLeft: (at + steps.length - 1) % steps.length, Home: 0, End: steps.length - 1 }[event.key];
  if (next === undefined) return;
  event.preventDefault();
  stopClock();
  showPhase(next);
  steps[next].focus();
});
reducedMotion.addEventListener("change", ({ matches }) => {
  if (!matches) return;
  stopClock();
  showPhase(2);
  remaining = 0;
});

const installTabs = select("#ins-tabs");
installTabs.addEventListener("click", (event) => {
  const tab = event.target.closest("[data-ins]");
  if (!tab) return;
  for (const button of all("[data-ins]")) button.setAttribute("aria-pressed", String(button === tab));
  for (const panel of all("[data-panel]")) panel.hidden = panel.dataset.panel !== tab.dataset.ins;
  for (const note of all("[data-install-note]")) note.hidden = !note.dataset.installNote.split(" ").includes(tab.dataset.ins);
});

const copyStatus = select("#copy-status");
const copyTimers = new WeakMap();
for (const button of all("[data-copy]")) {
  button.addEventListener("click", async () => {
    clearTimeout(copyTimers.get(button));
    const label = button.querySelector("span");
    copyStatus.textContent = "";
    let copied = false;
    try {
      await navigator.clipboard.writeText(document.getElementById(button.dataset.copy).textContent.trim());
      copied = true;
    } catch { /* The command remains selectable. */ }
    label.textContent = string(copied ? "copy.done" : "copy.fail");
    copyStatus.textContent = label.textContent;
    button.classList.toggle("done", copied);
    copyTimers.set(button, setTimeout(() => {
      label.textContent = string("copy.copy") ?? "Copy";
      button.classList.remove("done");
    }, 1500));
  });
  button.hidden = false;
}

function openLinkedFAQ() {
  if (location.hash === "#delivery-proof") select("#delivery-proof").open = true;
}
window.addEventListener("hashchange", openLinkedFAQ);
openLinkedFAQ();

if (!reducedMotion.matches && "IntersectionObserver" in window) {
  root.classList.add("motion");
  const observer = new IntersectionObserver((entries) => {
    for (const { isIntersecting, target } of entries) {
      if (!isIntersecting) continue;
      target.classList.add("in");
      observer.unobserve(target);
    }
  }, { rootMargin: "0px 0px -6% 0px" });
  for (const section of all(".reveal, .lane, .final")) observer.observe(section);
}

for (const button of languageButtons) {
  button.addEventListener("click", () => {
    setLanguage(button.dataset.lang);
    savePreference("specgate-landing-lang", root.lang);
  });
}
setLanguage(readPreference("specgate-landing-lang"));
select('[data-i18n-attr="aria-label:lang.aria"]').hidden = false;
theme.hidden = false;
installTabs.hidden = false;
controls.hidden = false;
scene.setAttribute("role", "tabpanel");
scene.setAttribute("aria-labelledby", steps[at].id);
scene.tabIndex = 0;
if (!reducedMotion.matches) play(true);
