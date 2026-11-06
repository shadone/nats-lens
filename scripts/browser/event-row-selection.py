#!/usr/bin/env python3
"""Trusted Chromium regression; requires existing agent-browser and running Vite.

Run: python3 scripts/browser/event-row-selection.py [http://127.0.0.1:18941]
The fixture imports the real EventRow and production styles. Ranges only measure
character positions; mouse down/move/up creates every selection under test.
"""
import json
import subprocess
import sys

BASE = sys.argv[1] if len(sys.argv) > 1 else "http://127.0.0.1:18941"
SESSION = "event-selection"


def browser(*args, source=None):
    result = subprocess.run(["agent-browser", "--session", SESSION, "--json", *map(str, args)],
                            input=source, text=True, capture_output=True, check=True)
    value = json.loads(result.stdout)
    assert value["success"], value
    return value.get("data", {})


def js(source):
    return browser("eval", "--stdin", source=source).get("result")


def is_open():
    return js("document.querySelector('.ev-toggle').getAttribute('aria-expanded')") == "true"


def drag(selector, start, end, expected):
    # Do not add a Range to Selection: only the trusted mouse may select text.
    points = js(f"""(() => {{
      getSelection().removeAllRanges();
      const el = document.querySelector({json.dumps(selector)});
      el.scrollIntoView({{block:'center'}});
      const node = el.firstChild;
      const point = (offset, end) => {{
        const r = document.createRange(); r.setStart(node, offset); r.setEnd(node, offset + 1);
        const b = r.getBoundingClientRect();
        return {{x: end ? b.right - .1 : b.left + .1, y:b.top + b.height / 2}};
      }};
      window.mouseEvidence = [];
      for (const type of ['mousedown','mousemove','mouseup','click'])
        document.addEventListener(type, e => window.mouseEvidence.push([type,e.isTrusted]), {{once:true}});
      return [point({start},false),point({end - 1},true)];
    }})()""")
    before = is_open()
    url = js("location.href")
    a, b = points
    browser("mouse", "move", round(a["x"]), round(a["y"]))
    browser("mouse", "down")
    for i in range(1, 9):
        browser("mouse", "move", round(a["x"] + (b["x"] - a["x"]) * i / 8),
                round(a["y"] + (b["y"] - a["y"]) * i / 8))
    browser("mouse", "up")
    evidence = js("({text:getSelection().toString(),events:window.mouseEvidence})")
    assert expected in evidence["text"], {"expected": expected, **evidence}
    assert is_open() == before, "Selection toggled row"
    assert js("location.href") == url, "Selection navigated"
    for event in ("mousedown", "mousemove", "mouseup"):
        assert [event, True] in evidence["events"], evidence
    return evidence["text"]


try:
    for width in (360, 761, 1280):
        browser("set", "viewport", width, 1000)
        browser("open", BASE + "/event-row.fixture.html?range=6h&system=shop")
        browser("wait", ".ev-toggle")
        browser("snapshot", "-i")
        for expanded in (False, True):
            assert is_open() == expanded
            for selector, start, end, expected in (
                (".ev .ty", 0, 16, "CONSUMER.DELETED"),
                (".ev .tx", 0, 69, "Synthetic consumer summary with words that wrap across several lines."),
                (".ev .tx", 70, 106, "synthetic" * 4),
            ):
                selected = drag(selector, start, end, expected)
                print(json.dumps({"width": width, "expanded": expanded, "selected": selected}), flush=True)
            # A subsequent trusted click clears the selection and toggles normally.
            browser("click", ".ev .ty")
            assert is_open() != expanded, "Click after selection did not toggle"
            if not expanded:
                browser("click", ".ev .ty")
                assert not is_open()
                browser("focus", ".ev-toggle")
                browser("press", "Enter")
                assert is_open(), "Enter did not expand"
        assert not is_open()
        # Stale selection elsewhere must not suppress activation, including keyboard.
        for selector in ("#elsewhere", ".ev:nth-of-type(2) .tx"):
            js(f"""(() => {{const r=document.createRange();r.selectNodeContents(document.querySelector({json.dumps(selector)}));getSelection().removeAllRanges();getSelection().addRange(r)}})()""")
            browser("focus", ".ev-toggle")
            browser("press", "Space")
            assert is_open(), "Space with stale selection did not expand"
            browser("click", ".ev .ty")
            assert not is_open()
        # Keyboard activation must also work with a selection inside this row.
        drag(".ev .tx", 0, 9, "Synthetic")
        browser("focus", ".ev-toggle")
        browser("press", "Enter")
        assert is_open()
        js("getSelection().removeAllRanges()")
        browser("press", "Space")
        assert not is_open()
        browser("click", ".ev .ty")
        links = js("Array.from(document.querySelectorAll('.ex a')).map(a=>a.getAttribute('href'))")
        assert any("id=synthetic-selection" in link and "range=6h" in link and "system=shop" in link for link in links), links
        assert js("document.querySelector('.ev-toggle').getAttribute('aria-controls') === document.querySelector('.ex').id")
        assert js("document.activeElement === document.querySelector('.ev-toggle')"), "Click lost focus"
        assert js("document.documentElement.scrollWidth <= innerWidth + 1"), "Long token overflows viewport"
        assert js("""Array.from(document.querySelectorAll('.ev .ty,.ev .tx')).every(el=>{
          const r=document.createRange();r.selectNodeContents(el);
          return Array.from(r.getClientRects()).every(b=>b.left>=0 && b.right<=innerWidth+1);
        })"""), "Text fragments overflow"
        drag(".sd-recent a", 0, 69, "Synthetic consumer summary with words that wrap across several lines.")
        print(f"PASS width={width}: drag, click, Enter/Space, stale selection, links, focus, wrapping", flush=True)
    browser("set", "device", "iPhone 12")
    browser("open", BASE + "/event-row.fixture.html")
    browser("wait", ".ev-toggle")
    browser("tap", ".ev .ty")
    assert is_open(), "Touch did not expand"
    browser("tap", ".ev .ty")
    assert not is_open(), "Touch did not collapse"
    print("PASS touch emulation: tap expands and collapses", flush=True)
finally:
    browser("close")
