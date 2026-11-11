"""Builds the interview guide pages for janecareers.com.

Writes plain HTML that search engines can read without running JavaScript:
  static/interview-questions/index.html            hub
  static/interview-questions/<role>/index.html     questions for one job
  static/interview-tips/<question>/index.html      how to answer one question
  static/guides.css, static/sitemap.xml, static/robots.txt

Usage: python3 build.py <path to internal/web/static>
"""
import html
import os
import sys
from datetime import date

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
from content_questions import QUESTIONS  # noqa: E402
from content_roles import ROLES  # noqa: E402

SITE = "https://www.janecareers.com"
TODAY = date.today().isoformat()
E = html.escape

LOGO = ('<svg aria-hidden="true" width="34" height="34" viewBox="0 0 64 64"><circle cx="32" cy="32" r="27" fill="none" '
        'stroke="#2a2a30" stroke-width="3"/><circle cx="32" cy="32" r="27" fill="none" stroke="#fff" stroke-width="3" '
        'stroke-linecap="round" stroke-dasharray="132 170" transform="rotate(-90 32 32)"/><circle cx="32" cy="5" r="2.2" '
        'fill="#fff"/><path d="M37 19v15.5a7.5 7.5 0 0 1-15 0" fill="none" stroke="#fff" stroke-width="5.5" '
        'stroke-linecap="round"/></svg>')

BEACON = ('<script type="module" src="https://static.cloudflareinsights.com/beacon.min.js" '
          'data-cf-beacon=\'{"token": "a8e5114f913f4ee68ca35ea90f0a7be5"}\'></script>')


def page(path, title, description, body):
    url = SITE + path
    return f"""<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>{E(title)}</title>
  <meta name="description" content="{E(description)}">
  <link rel="canonical" href="{url}">
  <meta property="og:type" content="article">
  <meta property="og:title" content="{E(title)}">
  <meta property="og:description" content="{E(description)}">
  <meta property="og:url" content="{url}">
  <link rel="icon" href="/favicon.ico" sizes="48x48">
  <link rel="icon" href="/favicon.svg" type="image/svg+xml">
  <link rel="apple-touch-icon" href="/apple-touch-icon.png">
  <link rel="preconnect" href="https://fonts.googleapis.com">
  <link rel="preconnect" href="https://fonts.gstatic.com" crossorigin>
  <link rel="stylesheet" href="https://fonts.googleapis.com/css2?family=Inter:wght@400;500;600;700;800&display=swap">
  <link rel="stylesheet" href="/guides.css">
</head>
<body>
  <header class="g-nav"><div class="g-wrap g-nav-in">
    <a class="g-brand" href="/" aria-label="Jane Careers">{LOGO}<span><b>Jane</b><small>Careers</small></span></a>
    <nav><a href="/interview-questions">Interview guides</a><a class="g-btn g-btn-sm" href="/signup">Get started</a></nav>
  </div></header>
  <main class="g-wrap g-main">
{body}
  </main>
  <footer class="g-foot"><div class="g-wrap">
    <a href="/">Jane Careers</a> · <a href="/interview-questions">Interview guides</a> · <a href="/pricing">Pricing</a>
    <p>Jane finds the jobs you're most qualified for and helps you ace the interview.</p>
  </div></footer>
  {BEACON}
</body>
</html>
"""


def cta(role=None):
    what = f"your {E(role)} interview" if role else "your interview"
    return f"""    <aside class="g-cta">
      <div>
        <h2>Practice {what} out loud</h2>
        <p>Reading answers isn't the same as saying them. Pick your interview stage, answer Jane's questions out loud, and get feedback on your answer, pace and filler words. Then try again.</p>
      </div>
      <a class="g-btn" href="/signup">Start practicing</a>
      <p class="g-small">Interview Prep is $8.99/month. Cancel anytime, 3-day money-back guarantee.</p>
    </aside>"""


def crumbs(*parts):
    items = ['<a href="/">Home</a>']
    for label, href in parts:
        items.append(f'<a href="{href}">{E(label)}</a>' if href else f"<span>{E(label)}</span>")
    return '    <p class="g-crumbs">' + " / ".join(items) + "</p>"


def role_page(r):
    path = f"/interview-questions/{r['slug']}"
    n = len(r["questions"])
    title = f"{r['role']} Interview Questions and Answers ({n} Common Questions)"
    desc = f"The {n} most common {r['role']} interview questions, what the interviewer wants to hear, and how to answer each one."
    qs = []
    for i, (q, want, how) in enumerate(r["questions"], 1):
        qs.append(f"""    <section class="g-q">
      <h2><span>{i}.</span> {E(q)}</h2>
      <p><b>What they want to know:</b> {E(want)}</p>
      <p><b>How to answer:</b> {E(how)}</p>
    </section>""")
    others = [x for x in ROLES if x["slug"] != r["slug"]][:6]
    related = "\n".join(f'      <li><a href="/interview-questions/{x["slug"]}">{E(x["role"])} interview questions</a></li>' for x in others)
    tips = "\n".join(f'      <li><a href="/interview-tips/{x["slug"]}">{E(x["short"])}</a></li>' for x in QUESTIONS[:5])
    body = f"""{crumbs(("Interview questions", "/interview-questions"), (r["role"], None))}
    <h1>{E(r['role'])} Interview Questions and Answers</h1>
    <p class="g-lead">{E(r['intro'])} Here are the {n} questions you're most likely to hear, and how to answer each one.</p>
    <p class="g-tip">Tip: for any “tell me about a time” question, use the <a href="/interview-tips/star-method">STAR method</a>.</p>
{chr(10).join(qs[:5])}
{cta(r['role'])}
{chr(10).join(qs[5:])}
    <section class="g-more">
      <h2>How to answer the classics</h2>
      <ul>
{tips}
      </ul>
      <h2>More interview guides</h2>
      <ul>
{related}
      </ul>
    </section>
{cta(r['role'])}"""
    return path, page(path, title, desc, body)


def question_page(q):
    path = f"/interview-tips/{q['slug']}"
    title = q["title"] + " (With Example)"
    desc = f"{q['intro']} A simple structure, an example answer and mistakes to avoid."
    steps = "\n".join(f"      <li><b>{E(a)}:</b> {E(b)}</li>" for a, b in q["steps"])
    mistakes = "\n".join(f"      <li>{E(m)}</li>" for m in q["mistakes"])
    roles = "\n".join(f'      <li><a href="/interview-questions/{r["slug"]}">{E(r["role"])} interview questions</a></li>' for r in ROLES[:8])
    others = "\n".join(f'      <li><a href="/interview-tips/{x["slug"]}">{E(x["short"])}</a></li>' for x in QUESTIONS if x["slug"] != q["slug"])
    body = f"""{crumbs(("Interview tips", "/interview-questions"), (q["short"], None))}
    <h1>{E(q['title'])}</h1>
    <p class="g-lead">{E(q['intro'])}</p>
    <section class="g-q">
      <h2>Why interviewers ask it</h2>
      <p>{E(q['why'])}</p>
    </section>
    <section class="g-q">
      <h2>A simple way to structure your answer</h2>
      <ol class="g-steps">
{steps}
      </ol>
    </section>
    <section class="g-q">
      <h2>Example answer</h2>
      <blockquote>{E(q['example'])}</blockquote>
      <p class="g-small">Make it yours: swap in your own job, numbers and goals.</p>
    </section>
    <section class="g-q">
      <h2>Mistakes to avoid</h2>
      <ul>
{mistakes}
      </ul>
    </section>
{cta()}
    <section class="g-more">
      <h2>More interview questions</h2>
      <ul>
{others}
      </ul>
      <h2>Interview questions by job</h2>
      <ul>
{roles}
      </ul>
    </section>"""
    return path, page(path, title, desc, body)


def hub_page():
    path = "/interview-questions"
    title = "Interview Questions and Answers by Job | Jane Careers"
    desc = "Common interview questions for office, healthcare, customer service, retail, tech and more, with how to answer each one."
    roles = "\n".join(f'      <li><a href="/interview-questions/{r["slug"]}"><b>{E(r["role"])}</b><span>{len(r["questions"])} questions</span></a></li>' for r in ROLES)
    qs = "\n".join(f'      <li><a href="/interview-tips/{q["slug"]}"><b>{E(q["short"])}</b><span>Structure and example</span></a></li>' for q in QUESTIONS)
    body = f"""{crumbs(("Interview questions", None))}
    <h1>Interview Questions and Answers</h1>
    <p class="g-lead">Know what they'll ask before you walk in. Pick your job to see the most common questions and how to answer each one, or learn how to handle the classic questions every interviewer asks.</p>
    <h2 class="g-h2">By job</h2>
    <ul class="g-grid">
{roles}
    </ul>
    <h2 class="g-h2">The classic questions</h2>
    <ul class="g-grid">
{qs}
    </ul>
{cta()}"""
    return path, page(path, title, desc, body)


CSS = """:root{--bg:#0b0b0c;--card:#151517;--line:#26262a;--text:#f4f4f5;--text2:#c4c4c8;--text3:#a1a1aa}
*{box-sizing:border-box}
body{margin:0;background:var(--bg);color:var(--text);font-family:Inter,system-ui,-apple-system,sans-serif;line-height:1.6;-webkit-font-smoothing:antialiased}
a{color:#fff}
.g-wrap{max-width:820px;margin:0 auto;padding:0 20px}
.g-nav{border-bottom:1px solid var(--line);position:sticky;top:0;background:rgba(11,11,12,.92);backdrop-filter:blur(8px);z-index:2}
.g-nav-in{display:flex;align-items:center;justify-content:space-between;gap:16px;padding-top:14px;padding-bottom:14px}
.g-brand{display:inline-flex;align-items:center;gap:10px;text-decoration:none}
.g-brand span{display:grid;line-height:1}
.g-brand b{font-size:20px;letter-spacing:-.03em}
.g-brand small{font-size:9.5px;font-weight:500;letter-spacing:.32em;color:var(--text3);margin-top:3px;text-transform:uppercase}
.g-nav nav{display:flex;align-items:center;gap:16px;font-size:14px}
.g-nav nav a{text-decoration:none;color:var(--text2)}
.g-btn{display:inline-block;background:#f4f4f5;color:#0b0b0c !important;font-weight:600;text-decoration:none;padding:13px 24px;border-radius:999px}
.g-btn-sm{padding:8px 16px;font-size:14px}
.g-main{padding-top:28px;padding-bottom:56px}
.g-crumbs{font-size:13px;color:var(--text3);margin:0 0 12px}
.g-crumbs a{color:var(--text3);text-decoration:none}
h1{font-size:clamp(30px,5vw,44px);line-height:1.1;letter-spacing:-.03em;margin:0 0 14px}
.g-lead{font-size:18px;color:var(--text2);margin:0 0 18px}
.g-tip{font-size:15px;color:var(--text2);background:var(--card);border:1px solid var(--line);border-radius:14px;padding:12px 16px}
.g-q{border-top:1px solid var(--line);padding:22px 0 6px}
.g-q h2{font-size:21px;line-height:1.3;letter-spacing:-.01em;margin:0 0 10px}
.g-q h2 span{color:var(--text3)}
.g-q p,.g-q li{color:var(--text2)}
.g-q b{color:var(--text)}
.g-steps li{margin-bottom:6px}
blockquote{margin:0;padding:16px 18px;background:var(--card);border:1px solid var(--line);border-radius:14px;color:var(--text);font-style:italic}
.g-cta{margin:30px 0;padding:24px;border-radius:22px;background:var(--card);border:1px solid #3a3a40}
.g-cta h2{margin:0 0 8px;font-size:23px;letter-spacing:-.02em}
.g-cta p{margin:0 0 16px;color:var(--text2)}
.g-small{font-size:13px;color:var(--text3) !important;margin:10px 0 0 !important}
.g-more{border-top:1px solid var(--line);margin-top:26px;padding-top:10px}
.g-more h2,.g-h2{font-size:19px;margin:22px 0 10px}
.g-more li{margin-bottom:6px}
.g-grid{list-style:none;padding:0;display:grid;grid-template-columns:repeat(auto-fill,minmax(220px,1fr));gap:10px}
.g-grid a{display:grid;gap:2px;padding:14px 16px;border-radius:14px;background:var(--card);border:1px solid var(--line);text-decoration:none}
.g-grid span{font-size:13px;color:var(--text3)}
.g-foot{border-top:1px solid var(--line);padding:24px 0 40px;font-size:14px;color:var(--text3)}
.g-foot a{color:var(--text2);text-decoration:none}
"""


def main(static):
    pages = [hub_page()] + [role_page(r) for r in ROLES] + [question_page(q) for q in QUESTIONS]
    for path, content in pages:
        out = os.path.join(static, path.strip("/"), "index.html")
        os.makedirs(os.path.dirname(out), exist_ok=True)
        with open(out, "w") as f:
            f.write(content)
    with open(os.path.join(static, "guides.css"), "w") as f:
        f.write(CSS)
    urls = ["/", "/pricing"] + [p for p, _ in pages]
    sm = ['<?xml version="1.0" encoding="UTF-8"?>', '<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">']
    sm += [f"  <url><loc>{SITE}{u}</loc><lastmod>{TODAY}</lastmod></url>" for u in urls]
    sm.append("</urlset>")
    with open(os.path.join(static, "sitemap.xml"), "w") as f:
        f.write("\n".join(sm) + "\n")
    with open(os.path.join(static, "robots.txt"), "w") as f:
        f.write(f"User-agent: *\nAllow: /\nDisallow: /app\nDisallow: /api/\n\nSitemap: {SITE}/sitemap.xml\n")
    print(len(pages), "pages")


if __name__ == "__main__":
    main(sys.argv[1])
