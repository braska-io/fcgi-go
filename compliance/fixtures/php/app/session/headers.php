<?php
// header(), header_remove(), headers_list() and Content-Type defaults.
require __DIR__ . '/../../_fixture.php';
$q = fixture('app/session/headers');

switch ($q['case'] ?? 'ctype_default') {
    case 'dup':
        header('X-Multi: one');
        header('X-Multi: two', false);
        header('X-Multi: three', false);
        break;
    case 'www_auth':
        http_response_code(401);
        header('WWW-Authenticate: Negotiate');
        header('WWW-Authenticate: NTLM', false);
        break;
    case 'replace':
        header('X-Rep: one');
        header('X-Rep: two');
        break;
    case 'remove':
        header('X-Foo: Bar');
        header('X-Bar: Baz');
        header_remove('X-Foo');
        break;
    case 'remove_ci':
        header('X-Foo: Bar');
        header('X-Bar: Baz');
        header_remove('x-foo');
        break;
    case 'remove_all':
        header('X-Foo: Bar');
        header('X-Bar: Baz');
        header_remove();
        // header_remove() also took X-Fixture; the suite needs it back.
        header('X-Fixture: app/session/headers');
        break;
    case 'remove_cookie':
        setcookie('a', '1');
        header_remove('Set-Cookie');
        break;
    case 'list':
        setcookie('foo', 'bar');
        header('Example-Test: foo');
        header('Content-Type: text/plain; charset=UTF-8');
        // X-Powered-By carries the PHP version, so it is left out.
        $lines = array_filter(headers_list(), fn ($h) => stripos($h, 'X-Powered-By:') !== 0);
        echo implode("\n", $lines);
        break;
    case 'ctype_default':
        echo 'x';
        break;
    case 'ctype_json':
        header('Content-Type: application/json');
        echo '{}';
        break;
    case 'ctype_charset':
        header('Content-Type: text/html; charset=ISO-8859-1');
        echo 'x';
        break;
    case 'ctype_ini':
        ini_set('default_mimetype', 'text/plain');
        echo 'x';
        break;
    case 'ctype_empty':
        ini_set('default_mimetype', '');
        echo 'x';
        break;
    case 'many':
        for ($i = 0; $i < 50; $i++) {
            header(sprintf('X-Many-%02d: %d', $i, $i));
        }
        break;
}
