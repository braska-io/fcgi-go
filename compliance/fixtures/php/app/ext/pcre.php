<?php
require __DIR__ . '/../../_fixture.php';
$q = fixture('app/ext/pcre');
header('Content-Type: text/plain; charset=UTF-8');

$s = "Gr\u{00FC}\u{00DF}e";
switch ($q['op'] ?? '') {
    case 'named':
        preg_match('/(?P<name>\w+): (?P<digit>\d+)/', 'foobar: 2008', $m);
        echo $m['name'], ',', $m['digit'], ',', $m[1], ',', count($m);
        break;
    case 'utf8_count':
        echo preg_match_all('/./u', $s), ',', preg_match_all('/./', $s);
        break;
    case 'bad_utf8':
        $r = preg_match('/./u', "\xC3\x28");
        echo var_export($r, true), ',', preg_last_error() === PREG_BAD_UTF8_ERROR ? 'PREG_BAD_UTF8_ERROR' : preg_last_error(), ',', preg_last_error_msg();
        break;
    case 'backtrack':
        ini_set('pcre.jit', '0');
        ini_set('pcre.backtrack_limit', '1000');
        $r = preg_match('/(?:\D+|<\d+>)*[!?]/', 'foobar foobar foobar');
        echo var_export($r, true), ',', preg_last_error() === PREG_BACKTRACK_LIMIT_ERROR ? 'PREG_BACKTRACK_LIMIT_ERROR' : preg_last_error();
        break;
    default:
        http_response_code(400);
        echo 'unknown op';
}
