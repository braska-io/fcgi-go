<?php
// Shared by every fixture: names the fixture and parses the query the way
// the contract defines it (RESPONDERS.md), independent of PHP's own parsing.

function fixture(string $name): array
{
    header('X-Fixture: ' . $name);
    $q = [];
    foreach (explode('&', $_SERVER['QUERY_STRING'] ?? '') as $pair) {
        if ($pair === '') {
            continue;
        }
        [$k, $v] = array_pad(explode('=', $pair, 2), 2, '');
        $k = urldecode($k);
        $v = urldecode($v);
        if (str_ends_with($k, '[]')) {
            $q[substr($k, 0, -2)][] = $v;
        } else {
            $q[$k] = $v;
        }
    }
    return $q;
}

function pattern(int $n): string
{
    $out = '';
    for ($i = 0; $i < $n; $i++) {
        $out .= chr($i % 256);
    }
    return $out;
}

function die_by(string $signal): void
{
    $numbers = ['SEGV' => 11, 'ABRT' => 6, 'KILL' => 9];
    posix_kill(getmypid(), $numbers[$signal] ?? 11);
    sleep(5); // the signal arrives asynchronously
}
