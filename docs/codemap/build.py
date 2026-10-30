"""Builds graphify-out/explorer.html, a map of the code to explore in a
browser, from the graph graphify made of the repository (/graphify writes
graphify-out/graph.json). The page is explorer.html beside this file, with
the graph written into it; it loads d3 from cdnjs and needs nothing else.

Run it from anywhere: python3 docs/codemap/build.py (or make codemap).
"""
import json
import os
import sys
from collections import Counter, defaultdict
from pathlib import Path

ROOT = Path(__file__).resolve().parents[2]
TEMPLATE = Path(__file__).resolve().with_name("explorer.html")
GRAPH = ROOT / "graphify-out/graph.json"
if not GRAPH.exists():
    sys.exit(f"{GRAPH} is missing: build the graph with /graphify first")
g = json.loads(GRAPH.read_text())

LAYERS = {
    "entry": ("Kirish nuqtalari", ["cmd/bot", "api", "serverless", "(root) vercel.json", "(root) docker-compose.yml", "(root) go.mod", "public"]),
    "wiring": ("Yig'ish va sozlash", ["internal/app", "internal/config", "internal/webhook", "internal/cron"]),
    "core": ("Yadro: xabar → javob", ["internal/service", "internal/agent", "internal/ai"]),
    "tools": ("Tool'lar va tashqi API", ["internal/tools", "internal/webapi", "internal/webapi/internal/genplaces", "internal/webapi/internal/placename", "internal/guards", "internal/gifs", "internal/stickers", "internal/doctext", "internal/doctext/testdata"]),
    "data": ("Saqlash", ["internal/storage"]),
    "telegram": ("Telegram API", ["internal/telegram"]),
    "games": ("O'yinlar va Mini App", ["internal/game", "internal/dooz", "internal/shashka", "internal/shashkaapp", "internal/miniapp", "public/game", "public/shashka", "public/stats"]),
    "docs": ("Hujjatlar va g'oyalar", ["(root) README.md", "docs/superpowers/specs", "docs/superpowers/plans"]),
}
LAYER_OF = {p: k for k, (_, ps) in LAYERS.items() for p in ps}

DESC = {
    "cmd/bot": "Long polling bilan ishga tushirish va migratsiya buyrug'i (make migrate).",
    "api": "Vercel funksiyalari: /api/webhook, /api/game, /api/shashka, /api/stats, /api/cron.",
    "serverless": "Vercel funksiyalari chaqiradigan handlerlar: lazy builder, sxema tekshiruvi.",
    "internal/app": "Botni qismlardan yig'adi: store, agent, tool'lar, menyular, Mini App'lar.",
    "internal/config": "Muhit o'zgaruvchilaridan sozlamalarni o'qiydi va tekshiradi.",
    "internal/webhook": "Telegram update'larini HTTP orqali qabul qiladi, secret bilan.",
    "internal/cron": "Kunlik tozalash ishini Vercel Cron uchun endpoint qiladi.",
    "internal/service": "Botning miyasi: update → buyruq, guard, limit, menyu yoki agentga uzatish.",
    "internal/agent": "Bitta suhbat navbati: tarix, model, tool chaqiruvlari, saqlash.",
    "internal/ai": "OpenAI-mos Chat Completions mijozi (Gemini, xAI).",
    "internal/tools": "Model chaqira oladigan funksiyalar va ularni tekshiruvchi registry.",
    "internal/webapi": "Jonli ma'lumot: valyuta, ob-havo, namoz, yangilik, futbol, kripto, MLBB.",
    "internal/webapi/internal/genplaces": "O'zbekiston joy nomlari jadvalini GeoNames'dan generatsiya qiladi.",
    "internal/webapi/internal/placename": "Joy nomlarining turli yozilishini bitta kalitga keltiradi.",
    "internal/guards": "Spam, xavfli link va nomaqbul rasmni tashqi xizmatlardan so'raydi.",
    "internal/gifs": "Kayfiyatga mos GIF'ni GIPHY'dan topadi.",
    "internal/stickers": "Stiker paketlarini import qiladi, model har birini bir marta ko'radi.",
    "internal/doctext": "Word, PDF va matn fayllaridan matnni ajratadi.",
    "internal/storage": "PostgreSQL: foydalanuvchi, suhbat, guruh, o'yin, reyting jadvallari.",
    "internal/telegram": "Bot API mijozi: polling, xabar yuborish, admin amallari.",
    "internal/game": "Tarozi: bozor tarozisi HTML5 o'yini, ochko serverda hisoblanadi.",
    "internal/dooz": "Dooz (X-O): butun o'yin tugma ma'lumotida, bot uch darajada.",
    "internal/shashka": "Rus shashkasi qoidalari va alpha-beta bot.",
    "internal/shashkaapp": "Shashka Mini App API: navbat, taklif, soat, Elo reyting.",
    "internal/miniapp": "Guruh statistikasi Mini App'i, init data HMAC bilan.",
    "public/game": "Tarozi o'yinining sahifasi.",
    "public/shashka": "Shashka Mini App sahifasi: lobbi, qidiruv, taxta.",
    "public/stats": "Guruh statistikasi dashboard sahifasi.",
    "(root) README.md": "Loyiha hujjati: arxitektura, xavfsizlik va dizayn qarorlari.",
    "docs/superpowers/specs": "Dizayn spetsifikatsiyalari: nega shunday qurilgan.",
    "docs/superpowers/plans": "Amalga oshirish rejalari, bosqichma-bosqich.",
}

SHORT = {
    "(root) README.md": "README", "(root) vercel.json": "vercel.json",
    "(root) docker-compose.yml": "docker-compose", "(root) go.mod": "go.mod",
    "docs/superpowers/specs": "specs", "docs/superpowers/plans": "plans",
    "internal/webapi/internal/genplaces": "genplaces", "internal/webapi/internal/placename": "placename",
    "internal/doctext/testdata": "doctext/testdata",
}

REL = {"calls": 0, "references": 1, "method": 2, "contains": 3, "implements": 4, "embeds": 4,
       "indirect_call": 0, "conceptually_related_to": 5, "semantically_similar_to": 5,
       "shares_data_with": 5, "rationale_for": 5, "cites": 5}


def package_of(sf):
    if not sf:
        return None
    d = os.path.dirname(sf)
    return d if d else "(root) " + sf


def kind_of(n):
    ft = n.get("file_type")
    if ft in ("concept", "rationale", "document"):
        return "d"
    label = n["label"]
    sf = n.get("source_file") or ""
    if label == os.path.basename(sf) or label == sf or ("/" in label and label.endswith((".go", ".js", ".html", ".css"))):
        return "x"
    if label.startswith("."):
        return "m"
    if label.endswith("()"):
        return "f"
    return "t"


nodes = [n for n in g["nodes"] if package_of(n.get("source_file"))]
index = {n["id"]: i for i, n in enumerate(nodes)}
pkgs = sorted({package_of(n["source_file"]) for n in nodes})
pkg_index = {p: i for i, p in enumerate(pkgs)}
files = sorted({n["source_file"] for n in nodes})
file_index = {f: i for i, f in enumerate(files)}

degree = Counter()
edges = []
pkg_links = defaultdict(lambda: [0, 0])
for e in g["links"]:
    rel = REL.get(e["relation"])
    s, t = index.get(e["source"]), index.get(e["target"])
    if rel is None or s is None or t is None or s == t:
        continue
    edges.append((s, t, rel))
    degree[s] += 1
    degree[t] += 1
    ps, pt = package_of(nodes[s]["source_file"]), package_of(nodes[t]["source_file"])
    if ps != pt and rel in (0, 1, 4, 5):
        key = (pkg_index[ps], pkg_index[pt])
        pkg_links[key][0] += 1
        if not nodes[s]["source_file"].endswith("_test.go"):
            pkg_links[key][1] += 1

out_nodes = []
for i, n in enumerate(nodes):
    loc = n.get("source_location") or ""
    head = loc[1:].split("-")[0] if loc.startswith("L") else ""
    out_nodes.append([
        n["label"][:90], pkg_index[package_of(n["source_file"])], file_index[n["source_file"]],
        int(head) if head.isdigit() else 0, 1 if n["source_file"].endswith("_test.go") else 0,
        degree[i], kind_of(n), (n.get("rationale") or "")[:400],
    ])

pkg_out = []
for p in pkgs:
    members = [i for i, n in enumerate(nodes) if package_of(n["source_file"]) == p]
    pkg_out.append({
        "id": p, "name": SHORT.get(p, p.replace("internal/", "")), "layer": LAYER_OF.get(p, "tools"),
        "desc": DESC.get(p, ""), "n": len(members),
        "tests": sum(out_nodes[i][4] for i in members),
        "files": len({nodes[i]["source_file"] for i in members}),
    })

hyper = []
for h in g.get("hyperedges", []):
    members = [index[m] for m in h.get("nodes", []) if m in index]
    if members:
        hyper.append({"label": h["label"], "nodes": members, "confidence": h.get("confidence", "")})

data = {
    "commit": g.get("built_at_commit", "")[:7],
    "layers": {k: v[0] for k, v in LAYERS.items()},
    "pkgs": pkg_out,
    "files": files,
    "nodes": out_nodes,
    "edges": [x for e in edges for x in e],
    "links": [[s, t, w[0], w[1]] for (s, t), w in pkg_links.items()],
    "hyper": hyper,
}
blob = json.dumps(data, ensure_ascii=False, separators=(",", ":")).replace("</", "<\\/")
html = TEMPLATE.read_text().replace("/*__DATA__*/null", blob)
(ROOT / "graphify-out/explorer.html").write_text(html)
print(f"{len(out_nodes)} nodes, {len(edges)} edges, {len(pkgs)} packages, {len(data['links'])} package links, "
      f"{len(html) / 1e6:.2f} MB")
