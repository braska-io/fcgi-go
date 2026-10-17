<?php
// Every request starts with fresh statics, globals and class properties,
// whichever child serves it and however often that child served before.
require __DIR__ . '/../../_fixture.php';
$q = fixture('app/runtime/state');
header('Content-Type: text/plain');

final class Counter
{
    public static int $n = 0;
}

function bump(): int
{
    static $calls = 0;
    return ++$calls;
}

$GLOBALS['fixture_counter'] = ($GLOBALS['fixture_counter'] ?? 0) + 1;
Counter::$n++;
$defined = defined('FIXTURE_ONCE') ? 'defined' : 'fresh';
define('FIXTURE_ONCE', 1);
echo bump(), ',', Counter::$n, ',', $GLOBALS['fixture_counter'], ',', $defined;
