<?php
// Allocates a string of mb MiB under memory_limit.
require __DIR__ . '/../../_fixture.php';
$q = fixture('app/runtime/memory');
header('Content-Type: text/plain');

$mb = (int) ($q['mb'] ?? 1);
echo 'start;';
$s = str_repeat('x', $mb * 1024 * 1024);
echo strlen($s), ';', memory_get_peak_usage() < 64 * 1024 * 1024 ? 'under' : 'over';
