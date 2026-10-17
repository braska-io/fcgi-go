<?php
require __DIR__ . '/../../_fixture.php';
$q = fixture('app/ext/fileinfo');
header('Content-Type: text/plain');

$finfo = new finfo(FILEINFO_MIME_TYPE);
switch ($q['op'] ?? '') {
    case 'png':
        $im = imagecreatetruecolor(2, 2);
        ob_start();
        imagepng($im);
        echo $finfo->buffer(ob_get_clean());
        break;
    case 'pdf':
        echo $finfo->buffer("%PDF-1.4\n%\xE2\xE3\xCF\xD3\n1 0 obj\n<<>>\nendobj\ntrailer\n<<>>\n%%EOF\n");
        break;
    case 'text_charset':
        echo (new finfo(FILEINFO_MIME))->buffer("hello world\n");
        break;
    default:
        http_response_code(400);
        echo 'unknown op';
}
