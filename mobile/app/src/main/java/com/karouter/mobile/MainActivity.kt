package com.karouter.mobile

import android.os.Bundle
import androidx.activity.ComponentActivity
import androidx.activity.compose.setContent
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.material3.Surface
import androidx.compose.runtime.*
import androidx.compose.ui.Modifier
import com.karouter.mobile.data.KaRouterClient
import com.karouter.mobile.ui.KaRouterTheme
import com.karouter.mobile.ui.LoginScreen
import com.karouter.mobile.ui.MainApp
import kotlinx.coroutines.launch

class MainActivity : ComponentActivity() {
    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        val client = KaRouterClient(applicationContext)

        setContent {
            var dark by remember { mutableStateOf(client.darkTheme) }
            var authenticated by remember { mutableStateOf(client.hasValidSession()) }
            var sessionNotice by remember { mutableStateOf("") }
            val scope = rememberCoroutineScope()

            LaunchedEffect(Unit) {
                if (authenticated) {
                    val ok = client.checkSession()
                    if (!ok) {
                        authenticated = false
                        sessionNotice = "Sesi 24 jam berakhir"
                    }
                }
            }

            KaRouterTheme(dark = dark) {
                Surface(modifier = Modifier.fillMaxSize()) {
                    if (!authenticated) {
                        LoginScreen(
                            initialUrl = client.baseUrl,
                            dark = dark,
                            bannerMsg = sessionNotice,
                            onToggleTheme = {
                                dark = !dark
                                client.darkTheme = dark
                            },
                            onLogin = { url, pw, onErr ->
                                scope.launch {
                                    try {
                                        client.login(url, pw)
                                        sessionNotice = ""
                                        authenticated = true
                                    } catch (e: Exception) {
                                        onErr(e.message ?: "Login gagal")
                                    }
                                }
                            }
                        )
                    } else {
                        MainApp(
                            client = client,
                            dark = dark,
                            onToggleTheme = {
                                dark = !dark
                                client.darkTheme = dark
                            },
                            onSessionExpired = { reason ->
                                client.clearSession()
                                sessionNotice = reason
                                authenticated = false
                            }
                        )
                    }
                }
            }
        }
    }
}
