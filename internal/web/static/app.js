"use strict";

/*
 * CaseMade web app.
 *
 * No framework and no build step: the Go server embeds and serves these files.
 * All DOM is built with the h() helper, which only ever sets text content, so
 * job descriptions, résumé text, and user input can never inject markup.
 */

const BRAND = "CaseMade";

// ---------------------------------------------------------------------------
// DOM helpers
// ---------------------------------------------------------------------------

function h(tag, attrs, ...children) {
  const el = document.createElement(tag);
  for (const [key, value] of Object.entries(attrs || {})) {
    if (value === undefined || value === null || value === false) continue;
    if (key === "class") el.className = value;
    else if (key.startsWith("on") && typeof value === "function") el.addEventListener(key.slice(2), value);
    else if (key === "value") el.value = value;
    else if (value === true) el.setAttribute(key, "");
    else el.setAttribute(key, String(value));
  }
  appendChildren(el, children);
  return el;
}

function appendChildren(el, children) {
  for (const child of children.flat(Infinity)) {
    if (child === null || child === undefined || child === false) continue;
    el.append(child instanceof Node ? child : document.createTextNode(String(child)));
  }
}

function mount(node) {
  document.getElementById("app").replaceChildren(node);
  window.scrollTo(0, 0);
}

function link(href, attrs, ...children) {
  return h("a", { href, ...attrs, onclick: (event) => {
    if (event.metaKey || event.ctrlKey || event.shiftKey || event.button !== 0) return;
    event.preventDefault();
    navigate(href);
  } }, ...children);
}

let toastTimer;
function toast(message, isError) {
  const el = document.getElementById("toast");
  el.textContent = message;
  el.className = isError ? "toast error" : "toast";
  el.hidden = false;
  clearTimeout(toastTimer);
  toastTimer = setTimeout(() => { el.hidden = true; }, 4200);
}

function spinnerRow(text) {
  return h("div", { class: "row muted" }, h("div", { class: "spinner" }), text);
}

function formatDate(value) {
  return new Date(value).toLocaleDateString(undefined, { dateStyle: "medium" });
}

// ---------------------------------------------------------------------------
// API
// ---------------------------------------------------------------------------

class ApiError extends Error {
  constructor(status, message, code) {
    super(message);
    this.status = status;
    this.code = code;
  }
}

async function api(path, options = {}) {
  const init = { method: options.method || "GET", headers: {}, credentials: "same-origin" };
  if (options.body !== undefined) {
    init.headers["Content-Type"] = "application/json";
    init.body = JSON.stringify(options.body);
  }

  const response = await fetch(path, init);
  const text = await response.text();
  let data = null;
  try { data = text ? JSON.parse(text) : null; } catch { data = null; }

  if (!response.ok) {
    if (response.status === 401 && !options.allowUnauthorized) {
      state.user = null;
      navigate("/login", { replace: true });
    }
    const message = (data && data.error) || text.trim() || `Request failed (${response.status})`;
    throw new ApiError(response.status, message, data && data.code);
  }

  return data;
}

// ---------------------------------------------------------------------------
// State
// ---------------------------------------------------------------------------

const state = {
  user: undefined,       // undefined = not loaded, null = signed out
  billing: null,
  plans: null,
  profile: null,
  preferences: null,
  resumes: null,
  resumeText: new Map(), // résumé id → text, fetched on demand
  lastSearch: null,      // { request, matches, resumeId }
};

function resetState() {
  Object.assign(state, {
    user: null, billing: null, profile: null, preferences: null,
    resumes: null, lastSearch: null,
  });
  state.resumeText.clear();
}

async function loadUser() {
  try {
    state.user = await api("/api/v1/me", { allowUnauthorized: true });
  } catch {
    state.user = null;
  }
  return state.user;
}

async function loadBilling() {
  try {
    state.billing = await api("/api/v1/billing/status");
  } catch (error) {
    if (error.status !== 401) console.error(error);
  }
  return state.billing;
}

async function loadPlans() {
  if (state.plans) return state.plans;
  try {
    state.plans = await api("/api/v1/billing/plans");
  } catch {
    state.plans = { billingEnabled: false, plans: [], freeJobMatchesPerWeek: 3, gmailEnabled: false };
  }
  return state.plans;
}

async function loadProfile(force) {
  if (!state.profile || force) state.profile = await api("/api/v1/career-profile");
  return state.profile;
}

async function loadPreferences(force) {
  if (!state.preferences || force) state.preferences = await api("/api/v1/preferences");
  return state.preferences;
}

async function savePreferences(patch) {
  const next = { ...(state.preferences || {}), ...patch };
  state.preferences = await api("/api/v1/preferences", { method: "PUT", body: {
    minimumSalary: next.minimumSalary || "",
    remote: !!next.remote,
    hybrid: !!next.hybrid,
    onsite: !!next.onsite,
    onboardingComplete: !!next.onboardingComplete,
  } });
  return state.preferences;
}

async function loadResumes(force) {
  if (!state.resumes || force) state.resumes = await api("/api/v1/resumes");
  return state.resumes;
}

async function resumeText(id) {
  if (!state.resumeText.has(id)) {
    const resume = await api(`/api/v1/resumes/${encodeURIComponent(id)}`);
    state.resumeText.set(id, resume.resumeText || "");
  }
  return state.resumeText.get(id);
}

const isPro = () => state.billing && state.billing.plan === "pro";

function refreshUsage() {
  const slot = document.querySelector(".sidebar-foot > div");
  if (slot) renderUsage(slot);
}

// ---------------------------------------------------------------------------
// Résumé PDF reading (in the browser, with Mozilla pdf.js)
// ---------------------------------------------------------------------------

let pdfjsPromise;
function loadPdfjs() {
  if (!pdfjsPromise) {
    pdfjsPromise = import("/vendor/pdfjs/pdf.min.mjs").then((pdfjs) => {
      pdfjs.GlobalWorkerOptions.workerSrc = "/vendor/pdfjs/pdf.worker.min.mjs";
      return pdfjs;
    });
  }
  return pdfjsPromise;
}

const MAX_RESUME_FILE_BYTES = 10 * 1024 * 1024;

async function extractResumeText(file) {
  if (file.size > MAX_RESUME_FILE_BYTES) throw new Error(`${file.name} is larger than 10 MB.`);

  if (file.type === "text/plain" || /\.txt$/i.test(file.name)) {
    return (await file.text()).trim();
  }
  if (file.type !== "application/pdf" && !/\.pdf$/i.test(file.name)) {
    throw new Error(`${file.name} isn't a PDF. Export your résumé as a PDF and try again.`);
  }

  const pdfjs = await loadPdfjs();
  const data = new Uint8Array(await file.arrayBuffer());
  const pdf = await pdfjs.getDocument({ data, isEvalSupported: false, disableFontFace: true }).promise;

  const pages = [];
  for (let number = 1; number <= Math.min(pdf.numPages, 20); number++) {
    const page = await pdf.getPage(number);
    const content = await page.getTextContent();

    const lines = [];
    let line = "";
    let lastY = null;
    let lastEnd = null;

    for (const item of content.items) {
      if (typeof item.str !== "string") continue;
      const x = item.transform[4];
      const y = item.transform[5];

      if (lastY !== null && Math.abs(y - lastY) > 2) {
        lines.push(line);
        line = "";
        lastEnd = null;
      } else if (lastEnd !== null && x - lastEnd > 1.5 && !line.endsWith(" ") && !item.str.startsWith(" ")) {
        line += " ";
      }

      line += item.str;
      lastY = y;
      lastEnd = x + (item.width || 0);

      if (item.hasEOL) {
        lines.push(line);
        line = "";
        lastY = null;
        lastEnd = null;
      }
    }
    if (line) lines.push(line);
    pages.push(lines.map((l) => l.replace(/\s+/g, " ").trim()).filter(Boolean).join("\n"));
  }

  return pages.join("\n\n").trim();
}

async function uploadResumeFiles(files) {
  const added = [];
  for (const file of files) {
    const text = await extractResumeText(file);
    const resume = await api("/api/v1/resumes", { method: "POST", body: { filename: file.name, resumeText: text } });
    state.resumeText.set(resume.id, text);
    added.push(resume);
  }
  await loadResumes(true);
  return added;
}

function resumeDropzone(onUploaded, { label = "Upload résumé PDFs", hint = "Select one or multiple files" } = {}) {
  const input = h("input", { type: "file", accept: "application/pdf,.pdf,.txt,text/plain", multiple: true, class: "visually-hidden", id: `resume-input-${Math.random().toString(36).slice(2)}` });
  const status = h("div", { class: "small muted", role: "status" });
  const zone = h("label", { class: "dropzone", for: input.id },
    h("span", { class: "dropzone-icon", "aria-hidden": "true" }, "⇪"),
    h("span", { class: "dropzone-title" }, label),
    h("span", { class: "faint small" }, hint),
  );

  const handle = async (fileList) => {
    const files = [...fileList];
    if (!files.length) return;
    zone.classList.add("busy");
    status.replaceChildren(spinnerRow(`Reading ${files.length === 1 ? files[0].name : `${files.length} files`}…`));
    try {
      const added = await uploadResumeFiles(files);
      status.replaceChildren(`Added ${added.length} résumé${added.length === 1 ? "" : "s"}.`);
      onUploaded && onUploaded(added);
    } catch (error) {
      status.replaceChildren(h("span", { class: "text-error" }, error.message));
    } finally {
      zone.classList.remove("busy");
      input.value = "";
    }
  };

  input.addEventListener("change", () => handle(input.files));
  zone.addEventListener("dragover", (event) => { event.preventDefault(); zone.classList.add("over"); });
  zone.addEventListener("dragleave", () => zone.classList.remove("over"));
  zone.addEventListener("drop", (event) => {
    event.preventDefault();
    zone.classList.remove("over");
    handle(event.dataTransfer.files);
  });

  return h("div", { class: "stack-sm" }, input, zone, status);
}

// ---------------------------------------------------------------------------
// Router
// ---------------------------------------------------------------------------

const routes = {
  "/": viewLanding,
  "/pricing": viewLanding,
  "/login": () => viewAuth("login"),
  "/signup": () => viewAuth("signup"),
  "/app": () => navigate("/app/jobs", { replace: true }),
  "/app/welcome": viewOnboarding,
  "/app/jobs": () => appPage("jobs", viewJobs),
  "/app/resumes": () => appPage("resumes", viewResumes),
  "/app/profile": () => appPage("profile", viewProfile),
  "/app/applications": () => appPage("applications", viewApplications),
  "/app/billing": () => appPage("billing", viewBilling),
};

function navigate(path, { replace } = {}) {
  if (replace) history.replaceState(null, "", path);
  else history.pushState(null, "", path);
  render();
}

window.addEventListener("popstate", render);

async function render() {
  const path = location.pathname.replace(/\/+$/, "") || "/";
  const view = routes[path];
  if (!view) {
    mount(viewNotFound());
    return;
  }
  await view();
  if (path === "/pricing") {
    const pricing = document.getElementById("pricing");
    if (pricing) pricing.scrollIntoView();
  }
}

// ---------------------------------------------------------------------------
// Marketing site
// ---------------------------------------------------------------------------

function brand() {
  return link("/", { class: "brand" }, h("span", { class: "brand-mark" }, "C"), BRAND);
}

function siteNav() {
  const signedIn = !!state.user;
  return h("header", { class: "site-nav" },
    h("div", { class: "container" },
      brand(),
      h("nav", { class: "row" },
        link("/pricing", { class: "btn btn-ghost btn-sm" }, "Pricing"),
        signedIn
          ? link("/app/jobs", { class: "btn btn-primary btn-sm" }, "Open app")
          : [
              link("/login", { class: "btn btn-ghost btn-sm" }, "Sign in"),
              link("/signup", { class: "btn btn-primary btn-sm" }, "Get started"),
            ],
      ),
    ),
  );
}

function siteFooter() {
  return h("footer", { class: "site-footer" },
    h("div", { class: "container row-between" },
      h("span", {}, `© ${new Date().getFullYear()} ${BRAND}`),
      h("span", {}, "Billing is handled securely by Stripe."),
    ),
  );
}

async function viewLanding() {
  if (state.user === undefined) await loadUser();
  const plansInfo = await loadPlans();

  const steps = [
    ["1", "Upload your résumé", "Drop in one or more PDFs. We read them in your browser and build your career profile from what's actually there."],
    ["2", "We check every requirement", "Each posting is split into its real requirements. Then we check every one against your experience, without assuming licenses or degrees you don't have."],
    ["3", "Apply where you fit", "See what you meet, what's partial, and what's missing. Then track every application from saved to offer."],
  ];

  mount(h("div", {},
    siteNav(),
    h("main", {},
      h("section", { class: "hero" },
        h("div", { class: "container" },
          h("span", { class: "pill eyebrow" }, "Less searching. Better matches."),
          h("h1", {}, "Upload your résumé.", h("br"), "We find the fit."),
          h("p", { class: "lead" }, `${BRAND} matches you to jobs using evidence from your real experience. It shows exactly why each role is a fit, and tracks every application in one place.`),
          h("div", { class: "row" },
            link(state.user ? "/app/jobs" : "/signup", { class: "btn btn-primary" }, state.user ? `Open ${BRAND}` : "Find my jobs"),
            link("/pricing", { class: "btn" }, "See pricing"),
          ),
        ),
      ),
      h("section", { class: "section" },
        h("div", { class: "container grid-3" },
          steps.map(([number, title, body]) =>
            h("div", { class: "card feature" },
              h("div", { class: "feature-icon", "aria-hidden": "true" }, number),
              h("h3", {}, title),
              h("p", { class: "muted" }, body),
            )),
        ),
      ),
      pricingSection(plansInfo),
    ),
    siteFooter(),
  ));
}

function pricingSection(plansInfo) {
  const freeLimit = plansInfo.freeJobMatchesPerWeek || 3;
  const plans = plansInfo.plans || [];

  const free = h("div", { class: "card price-card" },
    h("div", {}, h("h3", {}, "Free"), h("p", { class: "muted small" }, "Try evidence-based matching.")),
    h("div", { class: "price" }, "$0"),
    h("ul", { class: "ticks" },
      h("li", {}, `${freeLimit} job-match searches per week`),
      h("li", {}, "Résumé library and career profile"),
      h("li", {}, "Application tracker"),
      h("li", { class: "off" }, "Unlimited searches"),
      h("li", { class: "off" }, "Best-résumé recommendations"),
      h("li", { class: "off" }, "Gmail application sync"),
    ),
    link(state.user ? "/app/jobs" : "/signup", { class: "btn btn-block" }, state.user ? "Open app" : "Start free"),
  );

  const proFeatures = () => h("ul", { class: "ticks" },
    h("li", {}, "Unlimited job-match searches"),
    h("li", {}, "Résumé library and career profile"),
    h("li", {}, "Application tracker"),
    h("li", {}, "Best-résumé recommendation for every job"),
    h("li", {}, "Gmail application sync"),
  );

  const proCards = (plans.length ? plans : [{ key: "monthly", name: "Pro", displayPrice: "$2.99", interval: "month" }])
    .map((plan, index, all) => h("div", { class: index === all.length - 1 ? "card price-card featured" : "card price-card" },
      h("div", { class: "row-between" },
        h("div", {}, h("h3", {}, plan.name), h("p", { class: "muted small" }, "For an active job search.")),
        index === all.length - 1 && all.length > 1 ? h("span", { class: "pill pill-pro" }, "Best value") : null,
      ),
      h("div", { class: "price" }, plan.displayPrice, " ", h("small", {}, `/ ${plan.interval}`)),
      proFeatures(),
      h("button", {
        class: "btn btn-primary btn-block",
        disabled: !plansInfo.billingEnabled,
        onclick: () => startCheckout(plan.key),
      }, plansInfo.billingEnabled ? `Get ${plan.name}` : "Coming soon"),
    ));

  return h("section", { class: "section", id: "pricing" },
    h("div", { class: "container" },
      h("h2", { class: "section-title" }, "Simple pricing"),
      h("p", { class: "section-sub" }, "Start free. Upgrade when your search gets serious. Cancel anytime."),
      h("div", { class: proCards.length > 1 ? "grid-3" : "grid-2" }, free, proCards),
    ),
  );
}

async function startCheckout(planKey) {
  if (!state.user) {
    sessionStorage.setItem("casemade.pendingPlan", planKey);
    navigate("/signup");
    return;
  }
  try {
    const { url } = await api("/api/v1/billing/checkout", { method: "POST", body: { plan: planKey } });
    window.location.assign(url);
  } catch (error) {
    if (error.code === "already_subscribed") {
      toast("You already have Pro.");
      navigate("/app/billing");
      return;
    }
    toast(error.message, true);
  }
}

function viewNotFound() {
  return h("div", {}, siteNav(),
    h("div", { class: "auth-wrap" },
      h("div", { class: "empty" }, h("h3", {}, "Page not found"), link("/", { class: "btn" }, "Go home"))));
}

// ---------------------------------------------------------------------------
// Auth
// ---------------------------------------------------------------------------

async function viewAuth(mode) {
  if (state.user === undefined) await loadUser();
  if (state.user) {
    navigate("/app/jobs", { replace: true });
    return;
  }

  const isSignup = mode === "signup";
  const error = h("div", { class: "form-error hidden", role: "alert" });
  const submit = h("button", { class: "btn btn-primary btn-block", type: "submit" }, isSignup ? "Create account" : "Sign in");

  const form = h("form", { class: "stack", novalidate: true, onsubmit: async (event) => {
    event.preventDefault();
    error.classList.add("hidden");
    submit.disabled = true;

    const data = new FormData(form);
    const body = { email: data.get("email"), password: data.get("password") };
    if (isSignup) body.displayName = data.get("displayName") || "";

    try {
      const session = await api(isSignup ? "/api/v1/auth/signup" : "/api/v1/auth/login", {
        method: "POST", body, allowUnauthorized: true,
      });
      resetState();
      state.user = session.user;

      const pendingPlan = sessionStorage.getItem("casemade.pendingPlan");
      if (pendingPlan) {
        sessionStorage.removeItem("casemade.pendingPlan");
        await startCheckout(pendingPlan);
        return;
      }
      navigate(isSignup ? "/app/welcome" : "/app/jobs");
    } catch (err) {
      error.textContent = err.message;
      error.classList.remove("hidden");
      submit.disabled = false;
    }
  } },
    isSignup ? h("label", { class: "field" }, "Name", h("input", { type: "text", name: "displayName", autocomplete: "name", maxlength: 120 })) : null,
    h("label", { class: "field" }, "Email", h("input", { type: "email", name: "email", autocomplete: "email", required: true })),
    h("label", { class: "field" }, "Password",
      h("input", { type: "password", name: "password", autocomplete: isSignup ? "new-password" : "current-password", required: true, minlength: isSignup ? 10 : undefined }),
      isSignup ? h("span", { class: "faint small" }, "At least 10 characters.") : null,
    ),
    error,
    submit,
  );

  mount(h("div", {}, siteNav(),
    h("div", { class: "auth-wrap" },
      h("div", { class: "card auth-card stack" },
        h("div", { class: "stack-sm" },
          h("h1", {}, isSignup ? "Create your account" : "Welcome back"),
          h("p", { class: "muted" }, isSignup ? "Free to start. No card required." : "Sign in to continue your search."),
        ),
        form,
        h("p", { class: "muted small" },
          isSignup ? "Already have an account? " : `New to ${BRAND}? `,
          link(isSignup ? "/login" : "/signup", {}, isSignup ? "Sign in" : "Create an account"),
        ),
      ),
    ),
  ));
}

async function logout() {
  try { await api("/api/v1/auth/logout", { method: "POST", allowUnauthorized: true }); } catch { /* ignore */ }
  resetState();
  navigate("/");
}

// ---------------------------------------------------------------------------
// Onboarding: Welcome → Résumés → About you → Work preferences → Ready
// ---------------------------------------------------------------------------

async function viewOnboarding() {
  if (state.user === undefined) await loadUser();
  if (!state.user) {
    navigate("/login", { replace: true });
    return;
  }

  const shell = h("div", { class: "onboarding" });
  mount(h("div", {}, h("header", { class: "site-nav" }, h("div", { class: "container" }, brand(),
    h("button", { class: "btn btn-ghost btn-sm", onclick: () => finishOnboarding(false) }, "Skip setup"))), shell));

  shell.replaceChildren(spinnerRow("Loading…"));
  await Promise.all([loadProfile(true), loadPreferences(true), loadResumes(true)]);

  const STEPS = ["Résumé", "About you", "Preferences", "Ready"];
  let step = 0;
  const draft = {
    headline: state.profile.headline || "",
    summary: state.profile.summary || "",
    targetRole: state.profile.targetRole || "",
    location: state.profile.location || "",
    evidence: (state.profile.evidence || []).map((e) => ({ category: e.category, text: e.text })),
    minimumSalary: state.preferences.minimumSalary || "",
    remote: !!state.preferences.remote,
    hybrid: !!state.preferences.hybrid,
    onsite: !!state.preferences.onsite,
  };

  const stepper = () => h("ol", { class: "stepper", "aria-label": "Setup progress" },
    STEPS.map((label, index) => h("li", { class: index === step ? "active" : index < step ? "done" : "", "aria-current": index === step ? "step" : undefined }, label)));

  const go = (next) => { step = next; draw(); };

  const draw = () => {
    const body = [stepResume, stepAbout, stepPreferences, stepReady][step]();
    shell.replaceChildren(h("div", { class: "container narrow stack" }, stepper(), body));
    window.scrollTo(0, 0);
  };

  // Step 1: upload résumés
  const stepResume = () => {
    const list = h("div", { class: "stack-sm" });
    const drawList = () => list.replaceChildren(...(state.resumes || []).map((r) =>
      h("div", { class: "resume-row" }, h("span", { class: "resume-name" }, "📄 ", r.filename), r.isPrimary ? h("span", { class: "pill" }, "Default") : null)));
    drawList();

    const next = h("button", { class: "btn btn-primary", disabled: !(state.resumes || []).length, onclick: async () => {
      next.disabled = true;
      const primary = (state.resumes || []).find((r) => r.isPrimary) || state.resumes[0];
      try {
        const prefill = await api(`/api/v1/resumes/${encodeURIComponent(primary.id)}/profile-draft`);
        mergeDraft(draft, prefill, true);
      } catch (error) {
        toast(error.message, true);
      }
      go(1);
    } }, "Continue");

    return h("div", { class: "card stack" },
      h("div", { class: "stack-sm" },
        h("h1", { class: "onboarding-title" }, "Your résumé powers your matches"),
        h("p", { class: "muted" }, "Upload one or more résumés. We read them in your browser, keep only the text, and use it to build your profile. Add a version for each kind of role you're targeting."),
      ),
      resumeDropzone(() => { drawList(); next.disabled = false; }),
      list,
      h("div", { class: "row-between" },
        h("button", { class: "btn btn-ghost", onclick: () => go(1) }, "I'll type my profile instead"),
        next,
      ),
    );
  };

  // Step 2: review the profile built from the résumé
  const stepAbout = () => {
    const editor = profileEditor(draft);
    return h("div", { class: "stack" },
      h("div", { class: "card stack-sm" },
        h("h1", { class: "onboarding-title" }, "About you"),
        h("p", { class: "muted" }, "We pulled these from your résumé. Fix anything that's wrong, and remove anything that isn't true. Matches are only as honest as this profile."),
      ),
      editor.node,
      h("div", { class: "row-between" },
        h("button", { class: "btn btn-ghost", onclick: () => { Object.assign(draft, editor.read()); go(0); } }, "Back"),
        h("button", { class: "btn btn-primary", onclick: () => { Object.assign(draft, editor.read()); go(2); } }, "Continue"),
      ),
    );
  };

  // Step 3: what they're looking for
  const stepPreferences = () => {
    const role = h("input", { type: "text", maxlength: 200, value: draft.targetRole, placeholder: "e.g. Registered Nurse, Software Engineer" });
    const location = h("input", { type: "text", maxlength: 200, value: draft.location, placeholder: "e.g. Miami, FL" });
    const salary = h("input", { type: "text", inputmode: "numeric", maxlength: 20, value: draft.minimumSalary, placeholder: "Optional, e.g. 70000" });
    const checks = ["remote", "hybrid", "onsite"].map((key) => {
      const input = h("input", { type: "checkbox" });
      input.checked = draft[key];
      return [key, input];
    });
    const error = h("div", { class: "form-error hidden", role: "alert" });

    return h("div", { class: "card stack" },
      h("div", { class: "stack-sm" },
        h("h1", { class: "onboarding-title" }, "What are you looking for?"),
        h("p", { class: "muted" }, "We'll use these as your default search. You can change them anytime."),
      ),
      h("div", { class: "grid-2" },
        h("label", { class: "field" }, "Target role", role),
        h("label", { class: "field" }, "Location", location),
      ),
      h("label", { class: "field" }, "Minimum salary", salary),
      h("div", { class: "stack-sm" },
        h("span", { class: "field-label" }, "Work style"),
        h("div", { class: "row" }, checks.map(([key, input]) =>
          h("label", { class: "check" }, input, { remote: "Remote", hybrid: "Hybrid", onsite: "On-site" }[key]))),
      ),
      error,
      h("div", { class: "row-between" },
        h("button", { class: "btn btn-ghost", onclick: () => go(1) }, "Back"),
        h("button", { class: "btn btn-primary", onclick: async (event) => {
          draft.targetRole = role.value.trim();
          draft.location = location.value.trim();
          draft.minimumSalary = salary.value.trim();
          for (const [key, input] of checks) draft[key] = input.checked;

          const problems = [];
          if (!draft.targetRole) problems.push("Add a target role.");
          if (!draft.location) problems.push("Add a location.");
          if (!draft.remote && !draft.hybrid && !draft.onsite) problems.push("Choose at least one work style.");
          if (problems.length) {
            error.textContent = problems.join(" ");
            error.classList.remove("hidden");
            return;
          }

          event.currentTarget.disabled = true;
          try {
            await saveProfileDraft(draft);
            await savePreferences({ minimumSalary: draft.minimumSalary, remote: draft.remote, hybrid: draft.hybrid, onsite: draft.onsite });
            go(3);
          } catch (err) {
            error.textContent = err.message;
            error.classList.remove("hidden");
            event.currentTarget.disabled = false;
          }
        } }, "Save and continue"),
      ),
    );
  };

  // Step 4: done
  const stepReady = () => {
    const counts = {};
    for (const item of draft.evidence) counts[item.category] = (counts[item.category] || 0) + 1;
    return h("div", { class: "card stack center" },
      h("div", { class: "ready-mark", "aria-hidden": "true" }, "✓"),
      h("h1", { class: "onboarding-title" }, "Profile ready"),
      h("p", { class: "muted" }, `${draft.evidence.length} pieces of evidence: ${counts.experience || 0} experience, ${counts.skill || 0} skills, ${counts.education || 0} education. Searching for ${draft.targetRole} in ${draft.location}.`),
      h("div", { class: "row center" }, h("button", { class: "btn btn-primary", onclick: () => finishOnboarding(true) }, "Find my jobs")),
    );
  };

  draw();
}

async function finishOnboarding(autoSearch) {
  try { await savePreferences({ onboardingComplete: true }); } catch (error) { console.error(error); }
  if (autoSearch) sessionStorage.setItem("casemade.autoSearch", "1");
  navigate("/app/jobs");
}

// Merge a résumé-derived draft into the editable profile draft.
function mergeDraft(draft, prefill, onlyIfEmpty) {
  if (prefill.summary && (!onlyIfEmpty || !draft.summary)) draft.summary = prefill.summary;
  const existing = new Set(draft.evidence.map((e) => `${e.category}\u0000${e.text.toLowerCase()}`));
  for (const item of prefill.evidence || []) {
    const key = `${item.category}\u0000${item.text.toLowerCase()}`;
    if (!existing.has(key)) {
      draft.evidence.push({ category: item.category, text: item.text });
      existing.add(key);
    }
  }
}

async function saveProfileDraft(draft) {
  const evidence = [];
  for (const [category] of EVIDENCE_SECTIONS) {
    let index = 0;
    const seen = new Set();
    for (const item of draft.evidence.filter((e) => e.category === category)) {
      const text = item.text.trim();
      if (!text || seen.has(text)) continue;
      seen.add(text);
      evidence.push({ category, entryIndex: index++, text, source: `career_profile.${category}` });
    }
  }

  state.profile = await api("/api/v1/career-profile", {
    method: "PUT",
    body: {
      headline: (draft.headline || "").trim(),
      summary: (draft.summary || "").trim(),
      targetRole: (draft.targetRole || "").trim(),
      location: (draft.location || "").trim(),
      evidence,
    },
  });
  return state.profile;
}

// ---------------------------------------------------------------------------
// App shell
// ---------------------------------------------------------------------------

async function appPage(active, view) {
  if (state.user === undefined) await loadUser();
  if (!state.user) {
    navigate("/login", { replace: true });
    return;
  }

  const content = h("div", {}, spinnerRow("Loading…"));
  const usageSlot = h("div", {});
  mount(appShell(active, content, usageSlot));

  await Promise.all([loadBilling(), loadPreferences().catch(() => null)]);
  renderUsage(usageSlot);

  if (state.preferences && !state.preferences.onboardingComplete) {
    navigate("/app/welcome", { replace: true });
    return;
  }

  try {
    const node = await view();
    if (node) content.replaceChildren(node);
  } catch (error) {
    if (error.status === 401) return;
    content.replaceChildren(h("div", { class: "form-error" }, error.message));
  }
}

function appShell(active, content, usageSlot) {
  const items = [
    ["jobs", "/app/jobs", "Find jobs"],
    ["applications", "/app/applications", "My jobs"],
    ["resumes", "/app/resumes", "Résumés"],
    ["profile", "/app/profile", "Career profile"],
    ["billing", "/app/billing", "Plan & billing"],
  ];

  return h("div", { class: "shell" },
    h("aside", { class: "sidebar" },
      brand(),
      h("nav", { "aria-label": "App" },
        items.map(([key, href, label]) =>
          link(href, { class: key === active ? "nav-link active" : "nav-link", "aria-current": key === active ? "page" : undefined }, label)),
      ),
      h("div", { class: "sidebar-foot" },
        usageSlot,
        h("button", { class: "btn btn-ghost btn-sm", onclick: logout }, "Sign out"),
      ),
    ),
    h("main", { class: "main" }, content),
  );
}

function renderUsage(slot) {
  const billing = state.billing;
  if (!billing) { slot.replaceChildren(); return; }

  const usage = billing.jobMatches || { used: 0, limit: 0, period: "week" };
  const fill = h("span", {});
  fill.style.width = `${Math.min(100, (usage.used / Math.max(1, usage.limit)) * 100)}%`;

  slot.replaceChildren(h("div", { class: "usage-card" },
    h("div", { class: "row-between" },
      h("span", { class: "small muted" }, billing.plan === "pro" ? `${BRAND} Pro` : "Free plan"),
      billing.plan === "pro" ? h("span", { class: "pill pill-pro" }, "PRO") : null,
    ),
    billing.plan === "pro"
      ? h("span", { class: "small faint" }, "Unlimited searches")
      : [
          h("div", { class: "meter", role: "progressbar", "aria-valuemin": 0, "aria-valuemax": usage.limit, "aria-valuenow": usage.used }, fill),
          h("span", { class: "small faint" }, `${usage.used} of ${usage.limit} searches this ${usage.period}`),
          link("/app/billing", { class: "btn btn-primary btn-sm" }, "Upgrade"),
        ],
  ));
}

function pageHead(title, subtitle, ...actions) {
  return h("div", { class: "page-head row-between" },
    h("div", {}, h("h1", {}, title), subtitle ? h("p", {}, subtitle) : null),
    actions.length ? h("div", { class: "row" }, actions) : null,
  );
}

function upsell(title, body) {
  return h("div", { class: "card upsell" },
    h("div", {}, h("h3", {}, title), h("p", { class: "muted" }, body)),
    link("/app/billing", { class: "btn btn-primary" }, "Upgrade to Pro"),
  );
}

// ---------------------------------------------------------------------------
// Career profile
// ---------------------------------------------------------------------------

const EVIDENCE_SECTIONS = [
  ["experience", "Experience", "One role or accomplishment per entry. Include years where relevant."],
  ["skill", "Skills", "One skill per entry."],
  ["education", "Education", "Degrees and programs, e.g. “B.S. Computer Science, University of Florida”."],
  ["certification", "Certifications", ""],
  ["license", "Licenses", "Only list licenses you currently hold."],
  ["project", "Projects", ""],
];

// profileEditor renders an editable profile. read() returns the edited values.
function profileEditor(initial, { showRoleFields = false } = {}) {
  const headline = h("input", { type: "text", maxlength: 200, value: initial.headline || "", placeholder: "e.g. Backend engineer focused on distributed systems" });
  const summary = h("textarea", { maxlength: 5000, placeholder: "A short summary of your background." });
  summary.value = initial.summary || "";
  const targetRole = h("input", { type: "text", maxlength: 200, value: initial.targetRole || "", placeholder: "e.g. Software Engineer" });
  const location = h("input", { type: "text", maxlength: 200, value: initial.location || "", placeholder: "e.g. Miami, FL" });

  const lists = {};
  const sections = EVIDENCE_SECTIONS.map(([category, label, hint]) => {
    const list = h("div", { class: "evidence-list" });
    lists[category] = list;

    const addRow = (text) => {
      const field = h("textarea", { maxlength: 5000, rows: 1, "aria-label": label });
      field.value = text || "";
      const row = h("div", { class: "evidence-item" },
        field,
        h("button", { class: "btn btn-ghost btn-sm", type: "button", "aria-label": `Remove ${label} entry`, onclick: () => row.remove() }, "Remove"),
      );
      list.append(row);
      return field;
    };

    (initial.evidence || [])
      .filter((item) => item.category === category)
      .sort((a, b) => (a.entryIndex || 0) - (b.entryIndex || 0))
      .forEach((item) => addRow(item.text));

    return h("div", { class: "card stack-sm" },
      h("div", { class: "row-between" },
        h("div", {}, h("h3", {}, label), hint ? h("p", { class: "faint small" }, hint) : null),
        h("button", { class: "btn btn-sm", type: "button", onclick: () => addRow("").focus() }, "+ Add"),
      ),
      list,
    );
  });

  const node = h("div", { class: "stack" },
    h("div", { class: "card stack" },
      h("label", { class: "field" }, "Headline", headline),
      h("label", { class: "field" }, "Professional summary", summary),
      showRoleFields ? h("div", { class: "grid-2" },
        h("label", { class: "field" }, "Target role", targetRole),
        h("label", { class: "field" }, "Location", location),
      ) : null,
    ),
    sections,
  );

  const read = () => {
    const evidence = [];
    for (const [category] of EVIDENCE_SECTIONS) {
      for (const field of lists[category].querySelectorAll("textarea")) {
        const text = field.value.trim();
        if (text) evidence.push({ category, text });
      }
    }
    const values = { headline: headline.value.trim(), summary: summary.value.trim(), evidence };
    if (showRoleFields) {
      values.targetRole = targetRole.value.trim();
      values.location = location.value.trim();
    }
    return values;
  };

  return { node, read };
}

async function viewProfile() {
  const [profile, resumes] = await Promise.all([loadProfile(true), loadResumes(true)]);
  const container = h("div", {});

  const draw = (values) => {
    const editor = profileEditor(values, { showRoleFields: true });
    const save = h("button", { class: "btn btn-primary", onclick: async () => {
      save.disabled = true;
      try {
        await saveProfileDraft(editor.read());
        state.lastSearch = null;
        toast("Profile saved");
      } catch (error) {
        toast(error.message, true);
      } finally {
        save.disabled = false;
      }
    } }, "Save profile");

    const prefill = resumes.length
      ? h("div", { class: "card row-between" },
          h("div", {}, h("h3", {}, "Prefill from a résumé"), h("p", { class: "muted small" }, "Adds anything new from a résumé. Nothing is removed, and nothing is saved until you click Save.")),
          (() => {
            const select = h("select", { "aria-label": "Résumé to prefill from" }, resumes.map((r) => h("option", { value: r.id }, r.filename)));
            const primary = resumes.find((r) => r.isPrimary);
            if (primary) select.value = primary.id;
            const button = h("button", { class: "btn btn-sm", onclick: async () => {
              button.disabled = true;
              try {
                const current = { ...values, ...editor.read() };
                const prefillData = await api(`/api/v1/resumes/${encodeURIComponent(select.value)}/profile-draft`);
                mergeDraft(current, prefillData, true);
                draw(current);
                toast("Added evidence from your résumé. Review it, then save.");
              } catch (error) {
                button.disabled = false;
                toast(error.message, true);
              }
            } }, "Prefill");
            return h("div", { class: "row" }, select, button);
          })(),
        )
      : h("div", { class: "card row-between" },
          h("p", { class: "muted" }, "Have a résumé? Upload it and we'll fill this in for you."),
          link("/app/resumes", { class: "btn btn-sm" }, "Upload résumé"));

    container.replaceChildren(h("div", { class: "stack" }, prefill, editor.node, h("div", { class: "row" }, save)));
  };

  draw(profile);

  return h("div", {},
    pageHead("Career profile", "Matches are checked against what you put here. Only list what's true. That's what makes the scores trustworthy."),
    container,
  );
}

function profileToSearchProfile(profile) {
  const byCategory = (category) => (profile.evidence || [])
    .filter((item) => item.category === category)
    .sort((a, b) => a.entryIndex - b.entryIndex)
    .map((item) => item.text);

  return {
    about: [profile.headline, profile.summary].filter(Boolean).join("\n"),
    experience: byCategory("experience"),
    education: byCategory("education"),
    skills: byCategory("skill"),
    projects: byCategory("project"),
    certifications: byCategory("certification"),
    licenses: byCategory("license"),
  };
}

// ---------------------------------------------------------------------------
// Résumé library
// ---------------------------------------------------------------------------

async function viewResumes() {
  await loadResumes(true);
  const list = h("div", { class: "stack-sm" });

  const draw = () => {
    if (!state.resumes.length) {
      list.replaceChildren(h("div", { class: "card empty" }, h("h3", {}, "No saved résumés yet"), h("p", {}, "Upload a PDF above to get started.")));
      return;
    }
    list.replaceChildren(...state.resumes.map((resume) => {
      const actions = h("div", { class: "row" },
        resume.isPrimary
          ? h("span", { class: "pill" }, "Default")
          : h("button", { class: "btn btn-sm", onclick: async () => {
              try {
                await api(`/api/v1/resumes/${encodeURIComponent(resume.id)}/primary`, { method: "POST" });
                await loadResumes(true);
                draw();
              } catch (error) { toast(error.message, true); }
            } }, "Make default"),
        h("button", { class: "btn btn-sm", onclick: () => previewResume(resume) }, "View text"),
        h("button", { class: "btn btn-ghost btn-sm", onclick: async (event) => {
          const button = event.currentTarget;
          if (button.dataset.confirm !== "1") {
            button.dataset.confirm = "1";
            button.textContent = "Click again to delete";
            return;
          }
          try {
            await api(`/api/v1/resumes/${encodeURIComponent(resume.id)}`, { method: "DELETE" });
            state.resumeText.delete(resume.id);
            await loadResumes(true);
            draw();
            toast("Résumé deleted");
          } catch (error) { toast(error.message, true); }
        } }, "Delete"),
      );
      return h("div", { class: "card resume-card" },
        h("div", {}, h("div", { class: "resume-name" }, "📄 ", resume.filename), h("div", { class: "faint small" }, `Added ${formatDate(resume.createdAt)}`)),
        actions,
      );
    }));
  };

  draw();

  return h("div", { class: "stack" },
    pageHead("Résumés", "Keep a version for each kind of role. Your default résumé is used for searches, and Pro recommends the best one for every job."),
    h("div", { class: "card" }, resumeDropzone(() => { draw(); toast("Résumé added"); }, { label: "Upload another résumé", hint: "PDF, up to 10 MB. Only the text is kept." })),
    list,
  );
}

async function previewResume(resume) {
  try {
    const text = await resumeText(resume.id);
    openDialog(resume.filename, h("pre", { class: "resume-text" }, text || "(No text)"));
  } catch (error) {
    toast(error.message, true);
  }
}

// ---------------------------------------------------------------------------
// Dialog
// ---------------------------------------------------------------------------

function openDialog(title, ...content) {
  const dialog = h("dialog", { class: "dialog", "aria-label": title },
    h("div", { class: "dialog-head" },
      h("h2", {}, title),
      h("button", { class: "btn btn-ghost btn-sm", "aria-label": "Close", onclick: () => dialog.close() }, "✕"),
    ),
    h("div", { class: "dialog-body stack" }, content),
  );
  dialog.addEventListener("close", () => dialog.remove());
  dialog.addEventListener("click", (event) => { if (event.target === dialog) dialog.close(); });
  document.body.append(dialog);
  dialog.showModal();
  return dialog;
}

// ---------------------------------------------------------------------------
// Job matches
// ---------------------------------------------------------------------------

async function viewJobs() {
  const [profile, preferences, resumes] = await Promise.all([loadProfile(), loadPreferences(), loadResumes()]);
  const hasProfile = (profile.evidence || []).length > 0 || (profile.summary || "").trim() !== "";

  if (!hasProfile && !resumes.length) {
    return h("div", {},
      pageHead("Find jobs"),
      h("div", { class: "card empty" },
        h("h3", {}, "Start with your résumé"),
        h("p", {}, "Upload a résumé or build your profile so we can check each job's requirements against your experience."),
        h("div", { class: "row center" },
          link("/app/resumes", { class: "btn btn-primary" }, "Upload résumé"),
          link("/app/profile", { class: "btn" }, "Build profile"),
        ),
      ),
    );
  }

  const last = state.lastSearch && state.lastSearch.request;
  const targetRole = h("input", { type: "text", required: true, maxlength: 200, value: (last && last.targetRole) || profile.targetRole || "" });
  const location = h("input", { type: "text", required: true, maxlength: 200, value: (last && last.location) || profile.location || "" });
  const minimumSalary = h("input", { type: "text", inputmode: "numeric", placeholder: "Optional", value: (last && last.minimumSalary) || preferences.minimumSalary || "" });
  const remote = h("input", { type: "checkbox" });
  const hybrid = h("input", { type: "checkbox" });
  const onsite = h("input", { type: "checkbox" });
  const source = last || preferences;
  remote.checked = !!source.remote; hybrid.checked = !!source.hybrid; onsite.checked = !!source.onsite;

  // Which evidence to search with: the career profile plus an optional résumé.
  const resumeSelect = h("select", { "aria-label": "Résumé to search with" },
    hasProfile ? h("option", { value: "" }, "Career profile only") : null,
    resumes.map((r) => h("option", { value: r.id }, `${r.filename}${r.isPrimary ? " (default)" : ""}`)),
  );
  const defaultResume = resumes.find((r) => r.isPrimary);
  resumeSelect.value = state.lastSearch ? state.lastSearch.resumeId || "" : defaultResume ? defaultResume.id : "";
  if (!hasProfile && !resumeSelect.value && resumes.length) resumeSelect.value = resumes[0].id;

  const results = h("div", { class: "stack" });
  const submit = h("button", { class: "btn btn-primary", type: "submit" }, "Find matches");

  const runSearch = async () => {
    const resumeId = resumeSelect.value;
    const request = {
      careerProfile: profileToSearchProfile(profile),
      targetRole: targetRole.value.trim(),
      location: location.value.trim(),
      minimumSalary: minimumSalary.value.trim(),
      remote: remote.checked,
      hybrid: hybrid.checked,
      onsite: onsite.checked,
    };
    if (!request.targetRole || !request.location) {
      toast("Target role and location are required", true);
      return;
    }

    submit.disabled = true;
    results.replaceChildren(h("div", { class: "card" }, spinnerRow("Searching employers and checking every requirement. This can take up to 20 seconds…")));

    try {
      if (resumeId) request.resumeText = await resumeText(resumeId);
      const matches = await api("/api/v1/job-matches", { method: "POST", body: request });
      const { careerProfile, resumeText: _omit, ...shown } = request;
      state.lastSearch = { request: shown, matches: matches || [], resumeId };
      renderMatches(results, state.lastSearch.matches);
      savePreferences({ minimumSalary: request.minimumSalary, remote: request.remote, hybrid: request.hybrid, onsite: request.onsite }).catch(() => {});
    } catch (error) {
      results.replaceChildren(error.code === "free_limit_reached"
        ? upsell("You've used this week's free searches", "Upgrade to Pro for unlimited job-match searches.")
        : h("div", { class: "form-error" }, error.message));
    } finally {
      submit.disabled = false;
      await loadBilling();
      refreshUsage();
    }
  };

  const form = h("form", { class: "card stack", onsubmit: (event) => { event.preventDefault(); runSearch(); } },
    h("div", { class: "grid-3" },
      h("label", { class: "field" }, "Target role", targetRole),
      h("label", { class: "field" }, "Location", location),
      h("label", { class: "field" }, "Minimum salary", minimumSalary),
    ),
    h("div", { class: "row-between" },
      h("div", { class: "row" },
        h("label", { class: "check" }, remote, "Remote"),
        h("label", { class: "check" }, hybrid, "Hybrid"),
        h("label", { class: "check" }, onsite, "On-site"),
      ),
      h("div", { class: "row" },
        resumes.length ? h("label", { class: "field inline-field" }, "Search with", resumeSelect) : null,
        submit,
      ),
    ),
  );

  if (state.lastSearch) renderMatches(results, state.lastSearch.matches);

  const freeNotice = !isPro() && state.billing
    ? h("p", { class: "faint small" }, `Free plan: ${state.billing.jobMatches.used} of ${state.billing.jobMatches.limit} searches used this week.`)
    : null;

  if (sessionStorage.getItem("casemade.autoSearch")) {
    sessionStorage.removeItem("casemade.autoSearch");
    setTimeout(runSearch, 0);
  }

  return h("div", { class: "stack" },
    pageHead("Find jobs", "Ranked by how well your real experience covers each role's requirements."),
    form,
    freeNotice,
    results,
  );
}

function fitClass(match) {
  const level = (match.matchLevel || "").toLowerCase();
  if (level.includes("best") || level.includes("strong")) return "good";
  if (level.includes("good") || level.includes("moderate")) return "mid";
  return "low";
}

function renderMatches(container, matches) {
  if (!matches.length) {
    container.replaceChildren(h("div", { class: "card empty" },
      h("h3", {}, "No matching jobs were found"),
      h("p", {}, "Try a broader role title, a nearby city, or turn on remote roles."),
    ));
    return;
  }

  container.replaceChildren(
    h("p", { class: "muted" }, `${matches.length} role${matches.length === 1 ? "" : "s"}, best fit first`),
    ...matches.map(jobCard),
  );
}

function salaryText(match) {
  if (!match.salaryMin && !match.salaryMax) return null;
  const fmt = (n) => Number(n).toLocaleString(undefined, { maximumFractionDigits: 0 });
  const currency = match.salaryCurrency || "";
  const range = match.salaryMin && match.salaryMax ? `${fmt(match.salaryMin)}–${fmt(match.salaryMax)}` : fmt(match.salaryMin || match.salaryMax);
  return `${currency} ${range}${match.salaryPeriod ? ` / ${match.salaryPeriod}` : ""}`.trim();
}

function safeUrl(value) {
  try {
    const url = new URL(value);
    return url.protocol === "https:" || url.protocol === "http:" ? url.href : null;
  } catch {
    return null;
  }
}

function reqList(kind, title, items) {
  if (!items || !items.length) return null;
  return h("div", { class: "stack-sm" },
    h("span", { class: "section-label" }, `${title} (${items.length})`),
    h("ul", { class: `reqs ${kind}` }, items.map((text) => h("li", {}, text))),
  );
}

async function trackMatch(match) {
  const applyURL = safeUrl(match.applyURL || match.sourceURL || "");
  await api("/api/v1/applications/track", {
    method: "POST",
    body: {
      job: {
        id: match.id || "",
        source: match.source || "",
        title: match.title,
        company: match.company,
        location: match.location || "",
        description: (match.description || "").slice(0, 50000),
        applyURL: applyURL || "",
      },
    },
  });
}

function trackButton(match, extraClass) {
  const button = h("button", { class: `btn btn-sm ${extraClass || ""}`.trim(), onclick: async () => {
    button.disabled = true;
    try {
      await trackMatch(match);
      button.textContent = "Saved ✓";
      toast("Added to My jobs");
    } catch (error) {
      button.disabled = false;
      toast(error.message, true);
    }
  } }, "Save");
  return button;
}

function jobCard(match) {
  const cls = fitClass(match);
  const applyURL = safeUrl(match.applyURL || match.sourceURL || "");
  const meta = [match.location, match.workArrangement, match.employmentType, salaryText(match)].filter(Boolean);

  return h("article", { class: "card job-card" },
    h("div", { class: "job-head" },
      h("div", { class: "stack-sm" },
        h("div", {},
          h("button", { class: "job-title link-button", onclick: () => openJobDetail(match) }, match.title),
          h("div", { class: "muted" }, match.company),
        ),
        meta.length ? h("div", { class: "row" }, meta.map((m) => h("span", { class: "pill" }, m))) : null,
      ),
      h("div", { class: `score ${cls}`, title: match.matchLevel }, `${match.matchPercentage}%`),
    ),
    h("div", { class: "row" },
      h("span", { class: `pill pill-${cls}` }, match.matchLevel || "Fit"),
      h("span", { class: "faint small" },
        `${(match.supportedRequirements || []).length} met · ${(match.partialRequirements || []).length} partial · ${(match.missingRequirements || []).length} missing`),
    ),
    match.explanation ? h("p", { class: "muted" }, match.explanation) : null,
    h("div", { class: "row" },
      h("button", { class: "btn btn-sm", onclick: () => openJobDetail(match) }, "Job details"),
      applyURL ? h("a", { class: "btn btn-primary btn-sm", href: applyURL, target: "_blank", rel: "noopener noreferrer" }, "Apply ↗") : null,
      trackButton(match),
    ),
  );
}

function openJobDetail(match) {
  const applyURL = safeUrl(match.applyURL || match.sourceURL || "");
  const cls = fitClass(match);
  const required = (match.requirements || []).filter((r) => r.importance === "required").map((r) => r.text);
  const preferred = (match.requirements || []).filter((r) => r.importance === "preferred").map((r) => r.text);
  const recommendationSlot = h("div", {});

  openDialog(match.title,
    h("div", { class: "row-between" },
      h("div", {}, h("div", { class: "muted" }, match.company), h("div", { class: "faint small" }, [match.location, salaryText(match)].filter(Boolean).join(" · "))),
      h("div", { class: `score ${cls}` }, `${match.matchPercentage}%`),
    ),
    h("div", { class: "row" },
      applyURL ? h("a", { class: "btn btn-primary btn-sm", href: applyURL, target: "_blank", rel: "noopener noreferrer" }, "Apply ↗") : null,
      trackButton(match),
    ),
    h("div", { class: "card inset stack" },
      h("h3", {}, "Your match"),
      match.explanation ? h("p", { class: "muted" }, match.explanation) : null,
      reqList("supported", "You meet", match.supportedRequirements),
      reqList("partial", "Partially", match.partialRequirements),
      reqList("missing", "Missing", match.missingRequirements),
    ),
    recommendationCard(match, recommendationSlot),
    required.length || preferred.length ? h("div", { class: "stack-sm" },
      h("h3", {}, "Posting requirements"),
      required.length ? h("div", {}, h("span", { class: "section-label" }, "Required"), h("ul", { class: "plain-list" }, required.map((t) => h("li", {}, t)))) : null,
      preferred.length ? h("div", {}, h("span", { class: "section-label" }, "Preferred"), h("ul", { class: "plain-list" }, preferred.map((t) => h("li", {}, t)))) : null,
    ) : null,
    match.description ? h("div", { class: "stack-sm" }, h("h3", {}, "Job description"), h("div", { class: "description" }, match.description)) : null,
  );
}

// "Which résumé should I send?" Pro ranks every saved résumé for this job.
function recommendationCard(match, slot) {
  const resumes = state.resumes || [];
  if (resumes.length < 1) {
    slot.replaceChildren(h("div", { class: "card inset row-between" },
      h("p", { class: "muted small" }, "Upload your résumés and Pro will tell you which one fits this job best."),
      link("/app/resumes", { class: "btn btn-sm" }, "Add résumés")));
    return slot;
  }
  if (!isPro()) {
    slot.replaceChildren(h("div", { class: "card inset row-between" },
      h("div", {}, h("h3", {}, "Recommended résumé"), h("p", { class: "muted small" }, `Pro checks all ${resumes.length} of your résumés against this job and tells you which to send.`)),
      link("/app/billing", { class: "btn btn-primary btn-sm" }, "Upgrade")));
    return slot;
  }

  const button = h("button", { class: "btn btn-sm", onclick: async () => {
    button.disabled = true;
    slot.replaceChildren(h("div", { class: "card inset" }, spinnerRow("Comparing your résumés against this job…")));
    try {
      const inputs = await Promise.all(resumes.map(async (r) => ({ id: r.id, name: r.filename, resumeText: await resumeText(r.id) })));
      const result = await api("/api/v1/resume-recommendation", {
        method: "POST",
        body: {
          job: {
            id: match.id || "",
            title: match.title,
            company: match.company,
            description: match.description || "",
            requirements: match.requirements || [],
            isActive: true,
            applyURL: match.applyURL || "",
          },
          resumes: inputs,
        },
      });
      slot.replaceChildren(h("div", { class: "card inset stack-sm" },
        h("h3", {}, "Recommended résumé"),
        result.results.map((r, index) => h("div", { class: index === 0 ? "rec-row best" : "rec-row" },
          h("div", {}, h("div", { class: "resume-name" }, index === 0 ? "★ " : "", r.name), h("div", { class: "faint small" }, `${r.supportedRequirements.length} met · ${r.partialRequirements.length} partial · ${r.missingRequirements.length} missing`)),
          h("span", { class: `pill pill-${fitClass(r)}` }, `${r.matchPercentage}%`),
        )),
      ));
    } catch (error) {
      slot.replaceChildren(h("div", { class: "card inset" }, h("p", { class: "text-error small" }, error.code === "pro_required" ? "Résumé recommendation needs Pro." : `Résumé recommendation unavailable: ${error.message}`)));
    }
  } }, "Which résumé should I send?");

  slot.replaceChildren(h("div", { class: "card inset row-between" },
    h("div", {}, h("h3", {}, "Recommended résumé"), h("p", { class: "muted small" }, `Compare your ${resumes.length} résumé${resumes.length === 1 ? "" : "s"} against this job.`)),
    button));
  return slot;
}

// ---------------------------------------------------------------------------
// My jobs (application tracker)
// ---------------------------------------------------------------------------

const STATUS_LABELS = {
  saved: "Saved",
  applied: "Applied",
  received: "Received",
  recruiter_contact: "Recruiter contact",
  interviewing: "Interview",
  offer: "Offer",
  hired: "Hired",
  rejected: "Rejected",
  withdrawn: "Withdrawn",
};

const STATUS_EVENT = {
  saved: "status_changed",
  applied: "application_submitted",
  received: "application_received",
  recruiter_contact: "recruiter_contact",
  interviewing: "interview_scheduled",
  offer: "offer_received",
  hired: "hired",
  rejected: "rejected",
  withdrawn: "withdrawn",
};

const COLUMNS = [
  ["Saved", ["saved"]],
  ["Applied", ["applied", "received", "recruiter_contact"]],
  ["Interview", ["interviewing"]],
  ["Offer", ["offer", "hired"]],
  ["Closed", ["rejected", "withdrawn"]],
];

async function viewApplications() {
  const params = new URLSearchParams(location.search);
  if (params.get("gmail") === "connected") {
    history.replaceState(null, "", "/app/applications");
    toast("Gmail connected. Sync to update your applications.");
  }

  const [applications, plansInfo] = await Promise.all([api("/api/v1/applications"), loadPlans()]);
  const board = h("div", { class: "board" });

  const draw = () => {
    board.replaceChildren(...COLUMNS.map(([title, statuses]) => {
      const items = applications.filter((a) => statuses.includes(a.status));
      return h("section", { class: "column", "aria-label": title },
        h("div", { class: "column-head" }, h("span", {}, title), h("span", {}, items.length)),
        items.map((application) => applicationCard(application, (updated) => {
          Object.assign(application, updated);
          draw();
        })),
      );
    }));
  };
  draw();

  const reload = async () => {
    const fresh = await api("/api/v1/applications");
    applications.splice(0, applications.length, ...fresh);
    draw();
  };

  const body = applications.length
    ? board
    : h("div", { class: "card empty" },
        h("h3", {}, "Nothing tracked yet"),
        h("p", {}, "Hit “Save” on any job match to add it here."),
        h("div", { class: "row center" }, link("/app/jobs", { class: "btn btn-primary" }, "Find jobs")),
      );

  return h("div", { class: "stack" },
    pageHead("My jobs", "Every status change is saved to the job's timeline."),
    gmailCard(plansInfo, reload),
    body,
  );
}

function gmailCard(plansInfo, onSynced) {
  const card = h("div", { class: "card gmail-card" });

  if (!plansInfo.gmailEnabled) return null;

  if (!isPro()) {
    card.replaceChildren(
      h("div", {}, h("h3", {}, "Gmail sync"), h("p", { class: "muted small" }, "Pro reads recruiter emails, like confirmations, interview invites and rejections, and updates your jobs for you.")),
      link("/app/billing", { class: "btn btn-primary btn-sm" }, "Upgrade"),
    );
    return card;
  }

  const draw = async () => {
    card.replaceChildren(spinnerRow("Checking Gmail…"));
    let status;
    try {
      status = await api("/api/v1/email/gmail/status");
    } catch (error) {
      card.replaceChildren(h("p", { class: "text-error small" }, error.message));
      return;
    }

    if (!status.connected) {
      card.replaceChildren(
        h("div", {}, h("h3", {}, "Connect Gmail"), h("p", { class: "muted small" }, "Read-only access. We only look for job-application emails and update matching jobs on your board.")),
        h("a", { class: "btn btn-primary btn-sm", href: "/api/v1/email/gmail/connect" }, "Connect Gmail"),
      );
      return;
    }

    const sync = h("button", { class: "btn btn-primary btn-sm", onclick: async () => {
      sync.disabled = true;
      sync.textContent = "Syncing…";
      try {
        const result = await api("/api/v1/email/gmail/sync", { method: "POST" });
        toast(`Checked ${result.messages_fetched} emails: ${result.updated} job${result.updated === 1 ? "" : "s"} updated.`);
        await onSynced();
      } catch (error) {
        toast(error.message, true);
      } finally {
        sync.disabled = false;
        sync.textContent = "Sync now";
      }
    } }, "Sync now");

    const disconnect = h("button", { class: "btn btn-ghost btn-sm", onclick: async () => {
      if (disconnect.dataset.confirm !== "1") {
        disconnect.dataset.confirm = "1";
        disconnect.textContent = "Click again to disconnect";
        return;
      }
      try {
        await api("/api/v1/email/gmail/disconnect", { method: "DELETE" });
        toast("Gmail disconnected");
        draw();
      } catch (error) { toast(error.message, true); }
    } }, "Disconnect");

    card.replaceChildren(
      h("div", {}, h("h3", {}, "Gmail connected"), h("p", { class: "muted small" }, status.email_address ? `Watching ${status.email_address} for application updates.` : "Watching for application updates.")),
      h("div", { class: "row" }, sync, disconnect),
    );
  };

  draw();
  return card;
}

function applicationCard(application, onUpdate) {
  const job = application.job || {};
  const select = h("select", { "aria-label": "Status" },
    Object.entries(STATUS_LABELS).map(([value, label]) => h("option", { value }, label)));
  select.value = application.status;

  select.addEventListener("change", async () => {
    const newStatus = select.value;
    select.disabled = true;
    try {
      const result = await api(`/api/v1/applications/${encodeURIComponent(application.id)}/events`, {
        method: "POST",
        body: { eventType: STATUS_EVENT[newStatus], newStatus, summary: `Moved to ${STATUS_LABELS[newStatus]}.` },
      });
      onUpdate(result.application);
      toast(`Moved to ${STATUS_LABELS[newStatus]}`);
    } catch (error) {
      select.value = application.status;
      select.disabled = false;
      toast(error.message, true);
    }
  });

  const timeline = h("ul", { class: "timeline" });
  const details = h("details", { class: "more", ontoggle: async () => {
    if (!details.open || timeline.dataset.loaded) return;
    timeline.dataset.loaded = "1";
    timeline.replaceChildren(h("li", {}, "Loading…"));
    try {
      const events = await api(`/api/v1/applications/${encodeURIComponent(application.id)}/events`);
      timeline.replaceChildren(...(events.length
        ? events.map((e) => h("li", {}, `${formatDate(e.occurredAt)} · ${STATUS_LABELS[e.newStatus] || e.newStatus}${e.source === "email" ? " · from Gmail" : ""}${e.summary ? ` · ${e.summary}` : ""}`))
        : [h("li", {}, `Saved ${formatDate(application.createdAt)}`)]));
    } catch (error) {
      timeline.replaceChildren(h("li", {}, error.message));
    }
  } }, h("summary", { class: "small" }, "Timeline"), timeline);

  const applyURL = safeUrl(job.applyURL || "");

  return h("div", { class: "app-card" },
    h("div", {},
      h("div", { class: "title" }, job.title || "Untitled role"),
      h("div", { class: "muted small" }, [job.company, job.location].filter(Boolean).join(" · ")),
    ),
    select,
    applyURL ? h("a", { class: "small muted", href: applyURL, target: "_blank", rel: "noopener noreferrer" }, "Open posting ↗") : null,
    details,
  );
}

// ---------------------------------------------------------------------------
// Billing
// ---------------------------------------------------------------------------

async function viewBilling() {
  const params = new URLSearchParams(location.search);
  const checkout = params.get("checkout");
  if (checkout) {
    history.replaceState(null, "", "/app/billing");
    if (checkout === "success") {
      toast(`Payment received. Welcome to ${BRAND} Pro!`);
      // The webhook may land a moment after the redirect.
      for (let attempt = 0; attempt < 6 && !isPro(); attempt++) {
        await new Promise((resolve) => setTimeout(resolve, 1500));
        await loadBilling();
      }
      refreshUsage();
    }
  }

  const billing = state.billing || await loadBilling();
  const plansInfo = await loadPlans();
  const pro = isPro();

  const statusLine = (() => {
    if (!pro) return "Free plan";
    if (!billing.currentPeriodEnd) return "Active";
    const date = formatDate(billing.currentPeriodEnd);
    if (billing.cancelAtPeriodEnd) return `Cancels on ${date}`;
    if (billing.status === "past_due") return "Payment past due. Update your card to keep Pro.";
    return `Renews on ${date}`;
  })();

  const manage = h("button", { class: "btn", onclick: async () => {
    manage.disabled = true;
    try {
      const { url } = await api("/api/v1/billing/portal", { method: "POST" });
      window.location.assign(url);
    } catch (error) {
      manage.disabled = false;
      toast(error.message, true);
    }
  } }, "Manage billing");

  const current = h("div", { class: "card stack" },
    h("div", { class: "plan-row" },
      h("div", {},
        h("div", { class: "row" }, h("span", { class: "plan-name" }, pro ? `${BRAND} Pro` : "Free"), pro ? h("span", { class: "pill pill-pro" }, "PRO") : null),
        h("p", { class: "muted" }, statusLine),
      ),
      billing.canManageBilling ? manage : null,
    ),
    h("p", { class: "faint small" },
      pro
        ? `Unlimited job-match searches (fair-use limit ${billing.jobMatches.limit} per hour).`
        : `${billing.jobMatches.used} of ${billing.jobMatches.limit} free searches used this week.`),
  );

  const upgrade = !pro
    ? h("div", { class: "stack" },
        h("h2", {}, `Unlock ${BRAND} Pro`),
        !plansInfo.billingEnabled
          ? h("div", { class: "card muted" }, "Upgrades are coming soon.")
          : h("div", { class: "grid-2" }, plansInfo.plans.map((plan) => {
              const button = h("button", { class: "btn btn-primary btn-block", onclick: async () => {
                button.disabled = true;
                await startCheckout(plan.key);
                button.disabled = false;
              } }, `Choose ${plan.name}`);
              return h("div", { class: "card price-card" },
                h("h3", {}, plan.name),
                h("div", { class: "price" }, plan.displayPrice, " ", h("small", {}, `/ ${plan.interval}`)),
                h("ul", { class: "ticks" },
                  h("li", {}, "Unlimited job-match searches"),
                  h("li", {}, "Best-résumé recommendation for every job"),
                  h("li", {}, "Gmail application sync"),
                ),
                button,
              );
            })),
      )
    : null;

  const deleteButton = h("button", { class: "btn btn-danger btn-sm", onclick: async () => {
    if (deleteButton.dataset.confirm !== "1") {
      deleteButton.dataset.confirm = "1";
      deleteButton.textContent = "Click again to permanently delete";
      return;
    }
    deleteButton.disabled = true;
    try {
      await api("/api/v1/account", { method: "DELETE" });
      resetState();
      toast("Your account has been deleted");
      navigate("/");
    } catch (error) {
      deleteButton.disabled = false;
      toast(error.message, true);
    }
  } }, "Delete my account");

  const danger = h("div", { class: "card stack-sm" },
    h("h3", {}, "Delete account"),
    h("p", { class: "muted small" }, "Permanently deletes your profile, résumés and tracked jobs, and cancels any active subscription immediately."),
    h("div", { class: "row" }, deleteButton),
  );

  return h("div", { class: "stack" },
    pageHead("Plan & billing", state.user.email ? `Signed in as ${state.user.email}` : null),
    current,
    upgrade,
    danger,
  );
}

// ---------------------------------------------------------------------------

render();
