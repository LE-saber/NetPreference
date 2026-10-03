#!/usr/bin/env python3
"""Chromium interactions on the real custom editor and real Go preflight.

LuCI form/RPC/uci hosting is a documented API fixture, not a running router.
The separate rootfs test exercises the target's actual uci.js and /sbin/uci.
"""
import json
import os
from pathlib import Path
import shutil
import subprocess
from playwright.sync_api import sync_playwright

ROOT = Path(__file__).resolve().parents[1]
BASE = ROOT / 'files/www/luci-static/resources'
OUT = ROOT / 'dist/browser'
OUT.mkdir(parents=True, exist_ok=True)


def check_config(text):
    result = subprocess.run([str(ROOT / 'dist/netpreference'), 'rpc', 'call', 'check_config'],
                            input=json.dumps({'config': text}), text=True, capture_output=True, check=False)
    return json.loads(result.stdout)


with sync_playwright() as pw:
    browser = pw.chromium.launch(headless=True, executable_path=os.environ.get('CHROMIUM_BINARY') or shutil.which('chromium'),
                                 args=['--no-sandbox'])
    page = browser.new_page(viewport={'width': 1180, 'height': 900}, device_scale_factor=1)
    errors = []
    page.on('pageerror', lambda error: errors.append(str(error)))
    page.expose_function('npValidate', check_config)
    page.set_content('<html lang="zh-CN"><meta charset="utf-8"><style>body{font:14px system-ui,sans-serif;margin:24px;max-width:1120px}input,textarea,select,button{font:inherit;padding:7px;border:1px solid #bbb;border-radius:4px;box-sizing:border-box}button{cursor:pointer;background:#fff}h2{font-size:25px}.alert-message{padding:12px;border-left:3px solid #777}.cbi-button-action{font-weight:700;border-width:2px}</style><body><main id="root"></main></body></html>')
    page.add_script_tag(content=(ROOT / 'tests/luci_harness.js').read_text())
    page.evaluate('''() => {
      window.E=function(tag,attrs,children){
       const el=document.createElement(tag);attrs=attrs||{};
       Object.keys(attrs).forEach(k=>{const value=attrs[k];if(value==null)return;
        if(typeof value==='function')el.addEventListener(k,value);else el.setAttribute(k,String(value));
       });
       (Array.isArray(children)?children:[children]).forEach(c=>{if(c!=null)el.append(c);});return el;
      };
      window.h=NPTest.create(E,window.npValidate);
      delete h.sections.pc.profile;delete h.sections.work;delete h.sections.media;delete h.sections.bias;
    }''')
    page.evaluate('(s)=>{window.lib=h.module(s)}', (BASE / 'netpreference/library.js').read_text())
    page.evaluate('(s)=>{window.app=h.loadView(s,lib)}', (BASE / 'view/netpreference/advanced.js').read_text())
    page.evaluate('async()=>document.querySelector("#root").append(await app.render())')
    # Unicode escapes keep scripts portable on minimal host encodings.
    page.get_by_role('button', name='\u57df\u540d\u96c6 (0)', exact=True).click()
    page.locator('[data-action="add-set"]').click()
    page.get_by_label('\u57df\u540d\u96c6\u540d\u79f0').fill('AI\u670d\u52a1')
    page.get_by_label('\u57df\u540d\u5217\u8868').fill('openai.com\nanthropic.com\nhttps://claude.ai/new')
    page.screenshot(path=str(OUT / 'domain-sets.png'), full_page=True)
    page.get_by_role('button', name='\u6a21\u5f0f\u96c6 (0)', exact=True).click()
    page.locator('[data-action="add-profile"]').click()
    page.get_by_label('\u6a21\u5f0f\u96c6\u540d\u79f0').fill('\u65e5\u5e38\u6a21\u5f0f')
    profile = page.locator('[data-profile]')
    profile.locator('[data-action="add-rule"]').click()
    set_id = page.evaluate('app.model.sets[0].id')
    profile.locator('.np-rule').nth(0).get_by_label('\u8f93\u5165\u57df\u540d\u6216\u9009\u62e9\u57df\u540d\u96c6').select_option(set_id)
    profile.locator('[data-action="add-rule"]').click()
    r = profile.locator('.np-rule').nth(1)
    r.get_by_label('\u76f4\u63a5\u57df\u540d').fill('youtube.com')
    r.get_by_label('\u504f\u597d\u6a21\u5f0f').select_option('ipv4')
    profile.locator('[data-action="add-rule"]').click()
    r = profile.locator('.np-rule').nth(2)
    r.get_by_label('\u76f4\u63a5\u57df\u540d').fill('games.test')
    r.get_by_label('\u504f\u597d\u6a21\u5f0f').select_option('custom')
    r.get_by_label('A', exact=True).fill('0')
    r.get_by_label('B', exact=True).fill('150')
    r.get_by_label('\u4f18\u5148\u534f\u8bae\u65cf').select_option('ipv6')
    profile.locator('[data-action="add-rule"]').click()
    r = profile.locator('.np-rule').nth(3)
    r.get_by_label('\u76f4\u63a5\u57df\u540d').fill('example.com')
    r.get_by_label('\u504f\u597d\u6a21\u5f0f').select_option('dual')
    page.evaluate('async()=>{h.calls.length=0;await app.handleSaveApply()}')
    assert page.evaluate('h.notices.filter(x=>x[0]==="error").length') == 0
    assert page.evaluate('h.calls.map(c=>c[1])') == ['save', 'check_config', 'save', 'commit', 'apply']
    assert page.evaluate('app.model.profiles[0].rows.length') == 4
    assert page.evaluate('app.model.profiles[0].rows[2].wait_ms') == '0'
    assert page.evaluate('app.model.sets[0].original') == ['*.openai.com', '*.anthropic.com', '*.claude.ai']
    text = page.locator('body').inner_text()
    for internal in ('DomainSet ID', 'stable ID', 'Policy', 'Profile', set_id):
        assert internal not in text, internal
    page.screenshot(path=str(OUT / 'mode-sets-desktop.png'), full_page=True)
    page.set_viewport_size({'width': 390, 'height': 844})
    assert page.evaluate('document.documentElement.scrollWidth <= innerWidth+1'), 'mobile horizontal overflow'
    page.screenshot(path=str(OUT / 'mode-sets-mobile.png'), full_page=True)
    # Invalid custom timings are rejected before uci save/commit/apply.
    r = profile.locator('.np-rule').nth(2)
    r.get_by_label('A', exact=True).fill('750')
    r.get_by_label('B', exact=True).fill('500')
    page.evaluate('async()=>{h.calls.length=0;await app.handleSaveApply()}')
    assert page.evaluate('h.calls.map(c=>c[1])') == ['save', 'check_config']
    page.evaluate('async()=>await app.handleReset()')
    assert page.evaluate('app.model.profiles[0].rows[2].wait_ms') == '0'
    # When run after the kernel lab, render its actual CLI/RPC output without
    # replacing any numeric values. This closes the nft -> runtime -> RPC ->
    # browser chain; locally this block is explicitly skipped if no kernel lab.
    traffic_file = ROOT / 'dist/kernel-traffic.json'
    kernel_rendered = traffic_file.exists()
    if kernel_rendered:
        snapshot = json.loads(traffic_file.read_text())
        assert snapshot['enabled'] and snapshot['totals_available']
        row = next(r for r in snapshot['rows'] if r['host']['mac'] == '02:00:00:00:00:01')
        for family in ('ipv4', 'ipv6'):
            assert row[family + '_rate']['valid']
            assert row[family + '_rate']['upload_Bps'] > 0
            assert row[family + '_rate']['download_Bps'] > 0
        page.set_viewport_size({'width': 1280, 'height': 900})
        page.evaluate('(t)=>{h.responses.traffic=t;}', snapshot)
        page.evaluate('(s)=>{window.overview=h.loadView(s,lib);overview.statusBox=E("div");overview.trafficBox=E("div");document.querySelector("#root").replaceChildren(overview.statusBox,overview.trafficBox);}',
                      (BASE / 'view/netpreference/overview.js').read_text())
        page.evaluate('async()=>await overview.refresh()')
        cells = page.locator('table tr').nth(1).locator('td')
        assert cells.count() == 6
        for index in (1, 2, 3, 4):
            text = cells.nth(index).inner_text()
            assert '0 B/s' not in text and '\u7b49\u5f85' not in text and '\u2014' not in text, text
        assert cells.nth(5).inner_text() == f"{100 * row['ipv6_ratio']:.1f}%"
        page.screenshot(path=str(OUT / 'kernel-traffic-browser.png'), full_page=True)
    assert not errors, errors
    (OUT / 'result.json').write_text(json.dumps({'result': 'pass', 'browser': browser.version,
        'scope': 'real custom DOM + backend parser, LuCI hosting API fixture',
        'kernel_rpc_rendered': kernel_rendered,
        'checks': ['domain sets', 'four modes', 'no manual IDs', 'zero A/B', 'save/apply ordering',
                   'backend rejection', 'reset', 'desktop/mobile overflow']}, indent=2))
    browser.close()
print('PASS: Chromium domain-set and four-row mode-set interactions, backend validation, save/reset and mobile layout.')
