<?php
// A generated file download, written in 64 KiB chunks.
$left = (int) ($_GET['bytes'] ?? 1048576);
header('Content-Type: application/octet-stream');
header('Content-Length: ' . $left);
$chunk = str_repeat('x', 65536);
while ($left > 0) {
    $n = min($left, 65536);
    echo $n === 65536 ? $chunk : substr($chunk, 0, $n);
    $left -= $n;
}
