package com.karouter.mobile.ui

import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.darkColorScheme
import androidx.compose.material3.lightColorScheme
import androidx.compose.runtime.Composable
import androidx.compose.ui.graphics.Color

val LightBg = Color(0xFFE6EBF2)
val LightSurface = Color(0xFFECF1F7)
val LightInset = Color(0xFFDCE2EC)
val LightText = Color(0xFF1E293B)
val LightMuted = Color(0xFF64748B)
val LightAccent = Color(0xFF356DF3)

val DarkBg = Color(0xFF1D212B)
val DarkSurface = Color(0xFF232834)
val DarkInset = Color(0xFF161922)
val DarkText = Color(0xFFE2E8F0)
val DarkMuted = Color(0xFF94A3B8)
val DarkAccent = Color(0xFF5B8DEF)

val OkGreen = Color(0xFF16A34A)
val WarnAmber = Color(0xFFD97706)
val ErrRed = Color(0xFFDC2626)

private val DarkScheme = darkColorScheme(
    primary = DarkAccent,
    onPrimary = Color.White,
    background = DarkBg,
    onBackground = DarkText,
    surface = DarkSurface,
    onSurface = DarkText,
    surfaceVariant = DarkInset,
    onSurfaceVariant = DarkMuted,
    error = ErrRed
)

private val LightScheme = lightColorScheme(
    primary = LightAccent,
    onPrimary = Color.White,
    background = LightBg,
    onBackground = LightText,
    surface = LightSurface,
    onSurface = LightText,
    surfaceVariant = LightInset,
    onSurfaceVariant = LightMuted,
    error = ErrRed
)

@Composable
fun KaRouterTheme(dark: Boolean, content: @Composable () -> Unit) {
    MaterialTheme(
        colorScheme = if (dark) DarkScheme else LightScheme,
        content = content
    )
}
